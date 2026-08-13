// Package worker schedules one task at a time from each project in a fair
// round-robin queue. The queue lease and project lock make process crashes
// recoverable without a network service.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Duan-JM/LegalScout/internal/browser"
	"github.com/Duan-JM/LegalScout/internal/capture"
	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/logging"
	"github.com/Duan-JM/LegalScout/internal/sources"
	"github.com/Duan-JM/LegalScout/internal/store"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

const DefaultConcurrency = 1

type Engine struct {
	Workspace workspace.Workspace
	Queue     *store.QueueStore
	Browser   browser.Runner
	Holder    string
	Lease     time.Duration
	preflight map[string]error
	mu        sync.Mutex
}

func New(root workspace.Workspace, queue *store.QueueStore, runner browser.Runner) *Engine {
	return &Engine{
		Workspace: root, Queue: queue, Browser: runner,
		Holder: workerHolder(), Lease: 10 * time.Minute, preflight: make(map[string]error),
	}
}

func workerHolder() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", os.Getpid(), hex.EncodeToString(raw))
}

func MaxConcurrency() int {
	value, err := strconv.Atoi(os.Getenv("LEGALSCOUT_MAX_CONCURRENCY"))
	if err != nil || value < 1 {
		return DefaultConcurrency
	}
	if value > 8 {
		return 8
	}
	return value
}

// RunUntilIdle assigns every goroutine a distinct workspace-wide slot holder.
// It does only one project task per turn, then requeues the project so a large
// project cannot starve another.
func (e *Engine) RunUntilIdle(ctx context.Context) error {
	parallelism := MaxConcurrency()
	errs := make(chan error, parallelism)
	for index := 0; index < parallelism; index++ {
		instance := &Engine{
			Workspace: e.Workspace, Queue: e.Queue, Browser: e.Browser,
			Holder: workerHolder(), Lease: e.Lease, preflight: make(map[string]error),
		}
		go func(worker *Engine) { errs <- worker.runWorker(ctx) }(instance)
	}
	for index := 0; index < parallelism; index++ {
		if err := <-errs; err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) runWorker(ctx context.Context) (result error) {
	slots := MaxConcurrency()
	waitStep := e.Lease / 20
	if waitStep < 75*time.Millisecond {
		waitStep = 75 * time.Millisecond
	}
	if waitStep > time.Second {
		waitStep = time.Second
	}
	// SQLite leases are stored as Unix seconds, so allow one extra second for
	// truncation before deciding a crashed holder is still alive.
	deadline := time.Now().Add(e.Lease + time.Second + waitStep)
	var ok bool
	var err error
	for {
		ok, err = e.Queue.AcquireWorker(e.Holder, e.Lease, slots)
		if err != nil || ok {
			break
		}
		if time.Now().After(deadline) {
			return nil
		}
		timer := time.NewTimer(waitStep)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return err
	}
	defer func() {
		if err := e.Queue.ReleaseWorker(e.Holder); result == nil && err != nil {
			result = fmt.Errorf("release worker slot: %w", err)
		}
	}()

	const idlePoll = 75 * time.Millisecond
	const stableEmptyRounds = 2
	emptyRounds := 0
	for {
		ok, err := e.Queue.AcquireWorker(e.Holder, e.Lease, slots)
		if err != nil {
			return fmt.Errorf("renew worker slot: %w", err)
		}
		if !ok {
			return errors.New("worker slot lease was lost")
		}
		didWork, err := e.RunOnce(ctx)
		if err != nil {
			return err
		}
		if didWork {
			emptyRounds = 0
			continue
		}
		emptyRounds++
		if emptyRounds >= stableEmptyRounds {
			return nil
		}
		timer := time.NewTimer(idlePoll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (e *Engine) RunOnce(ctx context.Context) (bool, error) {
	item, err := e.Queue.ClaimProject(e.Lease)
	if err != nil {
		return false, err
	}
	if item == nil {
		return false, nil
	}
	project, err := e.Workspace.Resolve(item.ProjectID)
	if err != nil {
		queuedProject := workspace.Project{Path: item.ProjectPath}
		if queuedProject.IsArchiving() {
			if queueErr := e.Queue.Remove(item.ProjectID); queueErr != nil {
				return true, queueErr
			}
			return true, nil
		}
		if queueErr := e.Queue.Fail(item.ProjectID, fmt.Errorf("queued project cannot be resolved: %w", err)); queueErr != nil {
			return true, queueErr
		}
		return true, nil
	}
	projectStore, err := store.OpenProject(project)
	if err != nil {
		if queueErr := e.Queue.Fail(item.ProjectID, fmt.Errorf("open project state: %w", err)); queueErr != nil {
			return true, queueErr
		}
		return true, nil
	}
	defer projectStore.Close()
	locked, err := projectStore.AcquireLock("project", e.Holder, e.Lease)
	if err != nil {
		return true, err
	}
	if !locked {
		if err := e.Queue.Requeue(item.ProjectID); err != nil {
			return true, fmt.Errorf("requeue locked project: %w", err)
		}
		timer := time.NewTimer(75 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-timer.C:
		}
		return true, nil
	}
	if project.IsArchiving() {
		if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
			return true, fmt.Errorf("release archiving project lock: %w", err)
		}
		if err := e.Queue.Remove(item.ProjectID); err != nil {
			return true, fmt.Errorf("remove archiving project from queue: %w", err)
		}
		return true, nil
	}
	// An archive may have claimed the project after this worker opened its
	// SQLite file. Do not resurrect the original path after Queue.Remove.
	active, err := e.Queue.IsRunning(item.ProjectID)
	if err != nil {
		if releaseErr := projectStore.ReleaseLock("project", e.Holder); releaseErr != nil {
			return true, fmt.Errorf("check queue activity: %v; release project lock: %w", err, releaseErr)
		}
		return true, err
	}
	if !active {
		if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
			return true, fmt.Errorf("release cancelled project lock: %w", err)
		}
		return true, nil
	}
	task, err := projectStore.ClaimNext(e.Holder, e.Lease)
	if err != nil {
		return true, e.releaseThenRequeue(projectStore, item.ProjectID, err)
	}
	if task == nil {
		if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
			return true, fmt.Errorf("release completed project lock: %w", err)
		}
		if err := e.Queue.Finish(item.ProjectID); err != nil {
			return true, fmt.Errorf("finish completed project: %w", err)
		}
		return true, nil
	}
	logger, closeLog, logErr := logging.Open(project)
	if logErr == nil {
		defer closeLog()
		logger.Info("executing check", "task_id", task.ID, "source", task.SourceID, "subject", task.Subject)
	}
	if err := e.execute(ctx, project, projectStore, *task, false); err != nil {
		return true, e.releaseThenRequeue(projectStore, item.ProjectID, err)
	}
	// There may be more executable pending tasks. Requeueing after exactly one
	// preserves fair scheduling across projects. Releasing first is essential:
	// the next worker must not race this holder via reentrant locking.
	if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
		return true, fmt.Errorf("release project lock: %w", err)
	}
	if err := e.Queue.Requeue(item.ProjectID); err != nil {
		return true, fmt.Errorf("requeue project: %w", err)
	}
	return true, nil
}

func (e *Engine) releaseThenRequeue(projectStore *store.ProjectStore, projectID string, cause error) error {
	if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
		return fmt.Errorf("%v; release project lock: %w", cause, err)
	}
	if err := e.Queue.Requeue(projectID); err != nil {
		return fmt.Errorf("%v; requeue project: %w", cause, err)
	}
	return cause
}

func (e *Engine) execute(ctx context.Context, project workspace.Project, projectStore *store.ProjectStore, task domain.Task, manual bool) error {
	source, ok := sources.ByID(task.SourceID)
	if !ok {
		return projectStore.Complete(task.ID, domain.FatalError, "来源配置不存在，请联系维护者", "")
	}
	if err := e.preflightSource(ctx, source); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := browser.PreflightStatus(err)
		return projectStore.Complete(task.ID, status, "来源预检失败: "+err.Error(), "")
	}
	result, runErr := e.Browser.Run(ctx, source, task.Subject, manual)
	if runErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := result.Status
		if status.Validate() != nil || status == domain.Pending || status == domain.Running {
			status = domain.ClassifyError(runErr)
		}
		if len(result.Screenshot) > 0 {
			if _, err := capture.SaveDiagnostic(project.Path, source.ID, task.Subject, result.Screenshot, time.Now()); err != nil {
				return fmt.Errorf("save diagnostic screenshot: %w", err)
			}
		}
		return projectStore.Complete(task.ID, status, runErr.Error(), "")
	}
	if result.Status == domain.NeedsReview {
		return projectStore.Complete(task.ID, domain.NeedsReview, "需要人工完成验证码或确认页面", "")
	}
	if !result.Status.IsConfirmed() {
		return projectStore.Complete(task.ID, result.Status, "来源未返回可交付结果", "")
	}
	path, err := capture.Save(capture.Request{
		ProjectPath: project.Path, Sequence: task.Sequence, Subject: task.Subject,
		Source: source.Name, Status: result.Status, PNG: result.Screenshot, Now: time.Now(),
	})
	if err != nil {
		return projectStore.Complete(task.ID, domain.RetryableError, "保存截图失败: "+err.Error(), "")
	}
	if err := projectStore.Complete(task.ID, result.Status, "", path); err != nil {
		if removeErr := os.Remove(filepath.Join(project.Path, path)); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("complete task: %w (rollback screenshot: %v)", err, removeErr)
		}
		return fmt.Errorf("complete task: %w", err)
	}
	return nil
}

func (e *Engine) preflightSource(ctx context.Context, source sources.Adapter) error {
	e.mu.Lock()
	err, done := e.preflight[source.ID]
	e.mu.Unlock()
	if done {
		return err
	}
	err = e.Browser.Preflight(ctx, source)
	e.mu.Lock()
	if existing, exists := e.preflight[source.ID]; exists {
		err = existing
	} else {
		e.preflight[source.ID] = err
	}
	e.mu.Unlock()
	return err
}

// Review opens a visible browser for each needs-review task. It intentionally
// never attempts to solve a CAPTCHA; the user completes it and confirms with
// Enter before LegalScout reads and captures the result.
func (e *Engine) Review(ctx context.Context, project workspace.Project) (count int, result error) {
	if reviewer, ok := e.Browser.(interface{ ManualReviewError() error }); ok {
		if err := reviewer.ManualReviewError(); err != nil {
			return 0, err
		}
	}
	projectStore, err := store.OpenProject(project)
	if err != nil {
		return 0, err
	}
	defer projectStore.Close()
	locked, err := projectStore.AcquireLock("project", e.Holder, e.Lease)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, fmt.Errorf("项目正在由后台任务处理，请稍后再进行人工确认")
	}
	if project.IsArchiving() {
		if err := projectStore.ReleaseLock("project", e.Holder); err != nil {
			return 0, fmt.Errorf("项目正在归档，且释放人工确认锁失败: %w", err)
		}
		return 0, errors.New("项目正在归档，无法进行人工确认")
	}
	reviewCtx, stopReview := context.WithCancel(ctx)
	heartbeatErr := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go e.renewProjectLock(reviewCtx, projectStore, heartbeatErr, heartbeatDone, stopReview)
	defer func() {
		stopReview()
		<-heartbeatDone
		if releaseErr := projectStore.ReleaseLock("project", e.Holder); releaseErr != nil && result == nil {
			result = releaseErr
		}
	}()
	tasks, err := projectStore.List(domain.NeedsReview)
	if err != nil {
		return 0, err
	}
	for _, task := range tasks {
		select {
		case err := <-heartbeatErr:
			return count, err
		default:
		}
		if err := e.execute(reviewCtx, project, projectStore, task, true); err != nil {
			return count, err
		}
		select {
		case err := <-heartbeatErr:
			return count, err
		default:
		}
		updated, err := projectStore.List("")
		if err != nil {
			return count, err
		}
		for _, current := range updated {
			if current.ID == task.ID && current.Status.IsConfirmed() {
				count++
			}
		}
	}
	return count, nil
}

func (e *Engine) renewProjectLock(
	ctx context.Context,
	projectStore *store.ProjectStore,
	failures chan<- error,
	done chan<- struct{},
	cancel context.CancelFunc,
) {
	defer close(done)
	interval := e.Lease / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := projectStore.AcquireLock("project", e.Holder, e.Lease)
			if err != nil {
				cancel()
				select {
				case failures <- fmt.Errorf("续租人工确认锁: %w", err):
				default:
				}
				return
			}
			if !ok {
				cancel()
				select {
				case failures <- errors.New("人工确认期间项目锁已丢失"):
				default:
				}
				return
			}
		}
	}
}
