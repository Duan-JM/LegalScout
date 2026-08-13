// Package cli builds LegalScout's stable non-interactive command surface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Duan-JM/LegalScout/internal/browser"
	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/importer"
	"github.com/Duan-JM/LegalScout/internal/sources"
	"github.com/Duan-JM/LegalScout/internal/store"
	"github.com/Duan-JM/LegalScout/internal/ui"
	"github.com/Duan-JM/LegalScout/internal/worker"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

type Dependencies struct {
	Workspace   func() (workspace.Workspace, error)
	Runner      browser.Runner
	StartWorker func(string) error
	OpenPath    func(string) error
	Input       io.Reader
	Output      io.Writer
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		Workspace: workspace.Default, Runner: browser.New(), StartWorker: worker.StartDetached,
		OpenPath: openPath, Input: os.Stdin, Output: os.Stdout,
	}
}

func NewRoot(deps Dependencies) *cobra.Command {
	if deps.Workspace == nil {
		deps.Workspace = workspace.Default
	}
	if deps.Runner == nil {
		deps.Runner = browser.New()
	}
	if deps.StartWorker == nil {
		deps.StartWorker = worker.StartDetached
	}
	if deps.OpenPath == nil {
		deps.OpenPath = openPath
	}
	if deps.Output == nil {
		deps.Output = os.Stdout
	}
	root := &cobra.Command{
		Use:           "legalscout",
		Short:         "律师批量核查执行器",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			return ui.Panel(root, func(project workspace.Project) error {
				return enqueue(project, root, deps.StartWorker)
			}, func(project workspace.Project) error {
				return deps.OpenPath(project.ScreenshotsPath())
			}, func(name, input string) error {
				_, _, err := createProject(root, name, input)
				return err
			}, func(project workspace.Project) error {
				_, err := reviewProject(context.Background(), root, project, deps.Runner)
				return err
			})
		},
	}
	root.SetOut(deps.Output)
	root.AddCommand(newCommand(deps), startCommand(deps), statusCommand(deps), reviewCommand(deps), retryCommand(deps),
		openCommand(deps), archiveCommand(deps), doctorCommand(deps), workerCommand(deps))
	return root
}

func newCommand(deps Dependencies) *cobra.Command {
	var input, checklist string
	command := &cobra.Command{
		Use:   "new <项目名称>",
		Short: "创建隔离项目并导入名单",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if checklist != "securities" {
				return errors.New("目前仅支持 --checklist securities")
			}
			if input == "" {
				return errors.New("必须提供 --input 名单.xlsx 或名单.txt")
			}
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, count, err := createProject(root, args[0], input)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "已创建项目 %s（短 ID %s），导入 %d 个对象、%d 项任务。\n", project.Slug, project.ID, count, count*len(sources.Securities()))
			return nil
		},
	}
	command.Flags().StringVar(&input, "input", "", "名单 .xlsx 或 .txt 文件")
	command.Flags().StringVar(&checklist, "checklist", "securities", "核查清单")
	return command
}

func createProject(root workspace.Workspace, name, input string) (workspace.Project, int, error) {
	names, err := importer.ReadNames(input)
	if err != nil {
		return workspace.Project{}, 0, err
	}
	if len(names) == 0 {
		return workspace.Project{}, 0, errors.New("名单没有有效的非空名称")
	}
	project, err := root.Create(name)
	if err != nil {
		return workspace.Project{}, 0, err
	}
	cleanup := func(cause error) (workspace.Project, int, error) {
		if cleanupErr := os.RemoveAll(project.Path); cleanupErr != nil {
			return workspace.Project{}, 0, fmt.Errorf("%w；清理半成品项目失败: %v", cause, cleanupErr)
		}
		return workspace.Project{}, 0, cause
	}
	if err := copyInput(input, project.Path); err != nil {
		return cleanup(err)
	}
	state, err := store.OpenProject(project)
	if err != nil {
		return cleanup(err)
	}
	if err := state.Seed(names, sources.Securities()); err != nil {
		if closeErr := state.Close(); closeErr != nil {
			err = fmt.Errorf("%w；关闭项目状态失败: %v", err, closeErr)
		}
		return cleanup(err)
	}
	if err := state.Close(); err != nil {
		return cleanup(err)
	}
	return project, len(names), nil
}

func startCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "start [项目]",
		Short: "将项目加入后台队列并从断点继续",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, err := root.Resolve(first(args))
			if err != nil {
				return err
			}
			if err := enqueue(project, root, deps.StartWorker); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "已加入后台队列。任务会从 pending 状态继续执行。")
			return nil
		},
	}
}

func enqueue(project workspace.Project, root workspace.Workspace, launch func(string) error) error {
	state, err := store.OpenProject(project)
	if err != nil {
		return err
	}
	holder := fmt.Sprintf("enqueue-%d-%d", os.Getpid(), time.Now().UnixNano())
	locked, err := state.AcquireLock("project", holder, time.Minute)
	if err != nil {
		_ = state.Close()
		return err
	}
	if !locked {
		_ = state.Close()
		return errors.New("项目正在执行、人工确认或归档，请稍后重试")
	}
	defer func() {
		_ = state.ReleaseLock("project", holder)
		_ = state.Close()
	}()
	if project.IsArchiving() {
		return errors.New("项目正在归档，不能重新加入队列")
	}
	queue, err := store.OpenQueue(root.Root)
	if err != nil {
		return err
	}
	defer queue.Close()
	if err := queue.Enqueue(project); err != nil {
		return err
	}
	return launch(root.Root)
}

func statusCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "status [项目]",
		Short: "查看所有项目或单个项目的进度",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				project, err := root.Resolve(args[0])
				if err != nil {
					return err
				}
				return writeProjectStatus(cmd.OutOrStdout(), project)
			}
			projects, err := root.List()
			if err != nil {
				return err
			}
			if len(projects) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "尚无项目。")
				return nil
			}
			for _, project := range projects {
				if err := writeProjectStatus(cmd.OutOrStdout(), project); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func writeProjectStatus(output io.Writer, project workspace.Project) error {
	state, err := store.OpenProject(project)
	if err != nil {
		return err
	}
	defer state.Close()
	total, complete, pending, review, failed, err := state.Count()
	if err != nil {
		return err
	}
	statuses, err := state.Statuses()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%s [%s]：%s，完成 %d/%d，待执行 %d，需人工 %d，失败 %d\n",
		project.Name, project.Slug, projectStatusLabel(statuses), complete, total, pending, review, failed)
	return err
}

func projectStatusLabel(statuses []domain.CheckStatus) string {
	switch domain.ProjectStatusFor(statuses) {
	case domain.ProjectRunning:
		return "运行中"
	case domain.ProjectCompleted:
		return "已完成"
	case domain.ProjectNeedsReview:
		return "需人工处理"
	case domain.ProjectFailed:
		return "运行失败"
	case domain.ProjectQueued:
		return "排队中"
	default:
		return "草稿"
	}
}

func reviewCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "review [项目]",
		Short: "列出并在可见浏览器中处理需要人工确认的任务",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, err := root.Resolve(first(args))
			if err != nil {
				return err
			}
			count, err := reviewProject(context.Background(), root, project, deps.Runner)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "已处理 %d 项人工确认任务。\n", count)
			return nil
		},
	}
}

func reviewProject(ctx context.Context, root workspace.Workspace, project workspace.Project, runner browser.Runner) (int, error) {
	state, err := store.OpenProject(project)
	if err != nil {
		return 0, err
	}
	tasks, err := state.List(domain.NeedsReview)
	if closeErr := state.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	queue, err := store.OpenQueue(root.Root)
	if err != nil {
		return 0, err
	}
	defer queue.Close()
	return worker.New(root, queue, runner).Review(ctx, project)
}

func retryCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "retry [项目]",
		Short: "只重试可重试的失败项（fatal_error 需维护者修复来源）",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, err := root.Resolve(first(args))
			if err != nil {
				return err
			}
			state, err := store.OpenProject(project)
			if err != nil {
				return err
			}
			count, err := state.RetryFailures()
			_ = state.Close()
			if err != nil {
				return err
			}
			if count > 0 {
				if err := enqueue(project, root, deps.StartWorker); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "已重置 %d 个 retryable_error 项；fatal_error 未自动重试。\n", count)
			return nil
		},
	}
}

func openCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "open [项目]",
		Short: "在系统文件管理器中打开交付截图目录",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, err := root.Resolve(first(args))
			if err != nil {
				return err
			}
			return deps.OpenPath(project.ScreenshotsPath())
		},
	}
}

func archiveCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "archive [项目]",
		Short: "归档项目目录（不删除截图）",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			project, err := root.Resolve(first(args))
			if err != nil {
				return err
			}
			queue, err := store.OpenQueue(root.Root)
			if err != nil {
				return err
			}
			defer queue.Close()
			active, err := queue.IsRunning(project.ID)
			if err != nil {
				return err
			}
			if active {
				return errors.New("项目正在由后台任务处理，无法归档")
			}
			state, err := store.OpenProject(project)
			if err != nil {
				return err
			}
			holder := fmt.Sprintf("archive-%d-%d", os.Getpid(), time.Now().UnixNano())
			locked, err := state.AcquireLock("project", holder, 10*time.Minute)
			if err != nil {
				_ = state.Close()
				return err
			}
			if !locked {
				if closeErr := state.Close(); closeErr != nil {
					return fmt.Errorf("项目正在由后台任务处理，且关闭状态失败: %w", closeErr)
				}
				return errors.New("项目正在由后台任务处理，无法归档")
			}
			cleanupLocked := func(cause error) error {
				if releaseErr := state.ReleaseLock("project", holder); releaseErr != nil {
					cause = fmt.Errorf("%v；释放项目归档锁: %w", cause, releaseErr)
				}
				if closeErr := state.Close(); closeErr != nil {
					cause = fmt.Errorf("%v；关闭项目状态: %w", cause, closeErr)
				}
				return cause
			}
			active, err = queue.IsRunning(project.ID)
			if err != nil {
				return cleanupLocked(err)
			}
			if active {
				return cleanupLocked(errors.New("项目正在由后台任务处理，无法归档"))
			}
			if err := os.WriteFile(project.ArchivingPath(), []byte(time.Now().Format(time.RFC3339Nano)), 0o600); err != nil {
				return cleanupLocked(fmt.Errorf("标记项目归档状态: %w", err))
			}
			if err := state.ReleaseLock("project", holder); err != nil {
				_ = state.Close()
				_ = os.Remove(project.ArchivingPath())
				return fmt.Errorf("释放项目归档锁: %w", err)
			}
			if err := state.Close(); err != nil {
				_ = os.Remove(project.ArchivingPath())
				return fmt.Errorf("关闭项目状态: %w", err)
			}
			targetRoot := filepath.Join(root.Root, "_archive")
			if err := os.MkdirAll(targetRoot, 0o700); err != nil {
				_ = os.Remove(project.ArchivingPath())
				return err
			}
			target, err := archiveTarget(targetRoot, project.Slug, time.Now())
			if err != nil {
				_ = os.Remove(project.ArchivingPath())
				return err
			}
			if err := os.Rename(project.Path, target); err != nil {
				_ = os.Remove(project.ArchivingPath())
				return fmt.Errorf("归档项目: %w", err)
			}
			if err := queue.Remove(project.ID); err != nil {
				if rollbackErr := os.Rename(target, project.Path); rollbackErr != nil {
					return fmt.Errorf("从全局队列移除项目: %w；回滚归档目录: %v", err, rollbackErr)
				}
				_ = os.Remove(project.ArchivingPath())
				return fmt.Errorf("从全局队列移除项目: %w", err)
			}
			_ = os.Remove(filepath.Join(target, "_legalscout", "archiving"))
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "项目已归档至 %s\n", target)
			return nil
		},
	}
}

func archiveTarget(root, slug string, now time.Time) (string, error) {
	const maxSlugBytes = 200
	for len(slug) > maxSlugBytes {
		_, size := utf8.DecodeLastRuneInString(slug)
		slug = slug[:len(slug)-size]
	}
	base := filepath.Join(root, slug+"-"+now.Format("20060102-150405"))
	target := base
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			return target, nil
		} else if err != nil {
			return "", fmt.Errorf("检查归档目标: %w", err)
		}
		target = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func doctorCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "检查浏览器、工作区、SQLite 和来源连通性",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := deps.Workspace()
			if err != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "工作区：失败（%v）\n", err)
				return err
			}
			output := cmd.OutOrStdout()
			failed := false
			_, _ = fmt.Fprintf(output, "工作区：%s\n", root.Root)
			probe := filepath.Join(root.Root, "_legalscout", ".write-check")
			if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
				_, _ = fmt.Fprintf(output, "工作区权限：失败（%v）\n", err)
				failed = true
			} else {
				if err := os.Remove(probe); err != nil {
					_, _ = fmt.Fprintf(output, "工作区权限：失败（清理检查文件：%v）\n", err)
					failed = true
				} else {
					_, _ = fmt.Fprintln(output, "工作区权限：正常")
				}
			}
			queue, err := store.OpenQueue(root.Root)
			if err != nil {
				_, _ = fmt.Fprintf(output, "SQLite：失败（%v）\n", err)
				failed = true
			} else {
				if err := queue.Close(); err != nil {
					_, _ = fmt.Fprintf(output, "SQLite：失败（关闭：%v）\n", err)
					failed = true
				} else {
					_, _ = fmt.Fprintln(output, "SQLite：可写（纯 Go 驱动）")
				}
			}
			provider, isProvider := deps.Runner.(*browser.Provider)
			browserErr := error(nil)
			if !isProvider {
				_, _ = fmt.Fprintln(output, "浏览器：使用测试执行器")
			} else {
				version, err := provider.BrowserVersion(context.Background())
				browserErr = err
				if browserErr != nil {
					_, _ = fmt.Fprintf(output, "浏览器：失败（%v）\n", browserErr)
					failed = true
				} else {
					_, _ = fmt.Fprintf(output, "浏览器：%s；%s\n", version, provider.Description())
				}
			}
			results := make([]string, len(sources.Registry()))
			for index, source := range sources.Registry() {
				if err := source.PreflightContract(); err != nil {
					results[index] = fmt.Sprintf("%s：配置失败（%v）", source.Name, err)
					failed = true
					continue
				}
				if browserErr != nil {
					results[index] = fmt.Sprintf("%s：未连接（浏览器不可用）", source.Name)
					failed = true
					continue
				}
				ctx, stop := context.WithTimeout(context.Background(), 25*time.Second)
				err := deps.Runner.Preflight(ctx, source)
				stop()
				if err != nil {
					results[index] = fmt.Sprintf("%s：预检失败（%v）", source.Name, err)
				} else {
					results[index] = fmt.Sprintf("%s：预检通过", source.Name)
				}
			}
			for _, result := range results {
				_, _ = fmt.Fprintln(output, result)
				if strings.Contains(result, "失败") || strings.Contains(result, "未连接") {
					failed = true
				}
			}
			if failed {
				return errors.New("doctor 检测到失败项")
			}
			return nil
		},
	}
}

func workerCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:    "__worker",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := deps.Workspace()
			if err != nil {
				return err
			}
			queue, err := store.OpenQueue(root.Root)
			if err != nil {
				return err
			}
			defer queue.Close()
			return worker.New(root, queue, deps.Runner).RunUntilIdle(context.Background())
		},
	}
}

func copyInput(input, projectPath string) error {
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	name := filepath.Base(input)
	target, err := os.OpenFile(filepath.Join(projectPath, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func openPath(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", path)
	case "windows":
		command = exec.Command("explorer", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	return command.Start()
}
