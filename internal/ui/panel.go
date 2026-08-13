// Package ui contains the intentionally small Bubble Tea project panel.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/store"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

type projectRow struct {
	project workspace.Project
	status  domain.ProjectStatus
	total   int
	done    int
	review  int
}

type model struct {
	rows     []projectRow
	cursor   int
	message  string
	start    func(workspace.Project) error
	open     func(workspace.Project) error
	create   func(string, string) error
	review   func(workspace.Project) error
	root     workspace.Workspace
	creating bool
	field    int
	name     textinput.Model
	input    textinput.Model
}

func Panel(root workspace.Workspace, start func(workspace.Project) error, open func(workspace.Project) error,
	create func(string, string) error, review func(workspace.Project) error) error {
	_, err := tea.NewProgram(newModel(root, start, open, create, review)).Run()
	return err
}

func newModel(root workspace.Workspace, start func(workspace.Project) error, open func(workspace.Project) error,
	create func(string, string) error, review func(workspace.Project) error) model {
	name := textinput.New()
	name.Placeholder = "项目名称"
	name.CharLimit = 120
	input := textinput.New()
	input.Placeholder = "名单.xlsx 或 名单.txt 路径"
	input.CharLimit = 512
	return model{rows: loadRows(root), start: start, open: open, create: create, review: review, root: root, name: name, input: input}
}

func loadRows(root workspace.Workspace) []projectRow {
	projects, err := root.List()
	if err != nil {
		return nil
	}
	rows := make([]projectRow, 0, len(projects))
	for _, project := range projects {
		state, err := store.OpenProject(project)
		if err != nil {
			continue
		}
		total, done, _, review, _, _ := state.Count()
		statuses, _ := state.Statuses()
		_ = state.Close()
		rows = append(rows, projectRow{project: project, status: domain.ProjectStatusFor(statuses), total: total, done: done, review: review})
	}
	return rows
}

type refreshMsg time.Time

func refreshTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(value time.Time) tea.Msg { return refreshMsg(value) })
}

func (m model) Init() tea.Cmd { return refreshTick() }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := message.(refreshMsg); ok {
		m.rows = loadRows(m.root)
		if m.cursor >= len(m.rows) {
			m.cursor = max(0, len(m.rows)-1)
		}
		return m, refreshTick()
	}
	pressed, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.Matches(pressed, key.NewBinding(key.WithKeys("ctrl+c"))) {
		return m, tea.Quit
	}
	if m.creating {
		switch pressed.String() {
		case "esc":
			m.creating, m.message = false, "已取消创建"
			m.name.Blur()
			m.input.Blur()
			return m, nil
		case "enter":
			if m.field == 0 {
				if strings.TrimSpace(m.name.Value()) == "" {
					m.message = "请输入项目名称"
					return m, nil
				}
				m.field = 1
				m.name.Blur()
				m.input.Focus()
				return m, nil
			}
			if strings.TrimSpace(m.input.Value()) == "" {
				m.message = "请输入名单路径"
				return m, nil
			}
			if err := m.create(strings.TrimSpace(m.name.Value()), strings.TrimSpace(m.input.Value())); err != nil {
				m.message = "创建失败：" + err.Error()
				return m, nil
			}
			m.rows = loadRows(m.root)
			m.cursor, m.creating, m.field = max(0, len(m.rows)-1), false, 0
			m.name.Reset()
			m.input.Reset()
			m.input.Blur()
			m.message = "已创建 securities 项目"
			return m, nil
		}
		if m.field == 0 {
			var command tea.Cmd
			m.name, command = m.name.Update(pressed)
			return m, command
		}
		var command tea.Cmd
		m.input, command = m.input.Update(pressed)
		return m, command
	}
	switch pressed.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "r":
		if len(m.rows) > 0 {
			if err := m.start(m.rows[m.cursor].project); err != nil {
				m.message = "启动失败：" + err.Error()
			} else {
				m.message = "已加入后台队列"
			}
		}
		if key.Matches(pressed, key.NewBinding(key.WithKeys("q"))) {
			return m, tea.Quit
		}
	case "o":
		if len(m.rows) > 0 {
			if err := m.open(m.rows[m.cursor].project); err != nil {
				m.message = "打开失败：" + err.Error()
			}
		}
	case "n":
		m.creating, m.field, m.message = true, 0, "新建项目（清单：securities）"
		m.name.Focus()
	case "v":
		if len(m.rows) > 0 {
			if err := m.review(m.rows[m.cursor].project); err != nil {
				m.message = "人工确认失败：" + err.Error()
			} else {
				m.rows = loadRows(m.root)
				m.message = "人工确认已完成；已刷新进度"
			}
		}
	case "enter":
		if len(m.rows) > 0 {
			row := m.rows[m.cursor]
			m.message = fmt.Sprintf("详情：%s（%s） %s，完成 %d/%d，需人工 %d；目录：%s",
				row.project.Name, row.project.Slug, projectLabel(row.status), row.done, row.total, row.review, row.project.Path)
		}
	}
	return m, nil
}

func (m model) View() string {
	var builder strings.Builder
	builder.WriteString("LegalScout 项目面板\n\n")
	if m.creating {
		builder.WriteString("新建项目（默认清单：securities，Esc 取消）\n")
		builder.WriteString("项目名称：" + m.name.View() + "\n")
		builder.WriteString("名单路径：" + m.input.View() + "\n")
		builder.WriteString("[Enter] 下一项/创建\n")
		if m.message != "" {
			builder.WriteString("\n" + m.message)
		}
		return builder.String()
	}
	if len(m.rows) == 0 {
		builder.WriteString("尚无项目。按 N 新建项目，或按 Q 退出。\n")
	} else {
		builder.WriteString("项目                 状态          进度       待处理\n")
		for index, row := range m.rows {
			cursor := " "
			if index == m.cursor {
				cursor = ">"
			}
			fmt.Fprintf(&builder, "%s %-20s %-12s %d/%d       %d\n", cursor, row.project.Name, projectLabel(row.status), row.done, row.total, row.review)
		}
	}
	builder.WriteString("\n[N] 新建  [Enter] 详情  [R] 启动/继续  [V] 人工确认  [O] 打开截图  [Q] 退出")
	if m.message != "" {
		builder.WriteString("\n" + m.message)
	}
	return builder.String()
}

func max(first, second int) int {
	if first > second {
		return first
	}
	return second
}

func projectLabel(status domain.ProjectStatus) string {
	labels := map[domain.ProjectStatus]string{
		domain.ProjectDraft: "草稿", domain.ProjectQueued: "排队中", domain.ProjectRunning: "运行中",
		domain.ProjectNeedsReview: "需人工处理", domain.ProjectCompleted: "已完成", domain.ProjectFailed: "运行失败",
	}
	return labels[status]
}
