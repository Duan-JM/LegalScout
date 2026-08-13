package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

func keyRune(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}

func TestPanelUpdateCreatesReviewsAndShowsDetails(t *testing.T) {
	created := ""
	reviewed := false
	row := projectRow{project: workspace.Project{Name: "项目 A", Slug: "project-a", Path: "/workspace/project-a"},
		status: domain.ProjectNeedsReview, total: 8, done: 3, review: 2}
	m := newModel(workspace.Workspace{Root: "/definitely-missing"}, nil, nil, nil, nil)
	m.rows = []projectRow{row}
	m.create = func(name, input string) error {
		created = name + "|" + input
		return nil
	}
	m.review = func(workspace.Project) error {
		reviewed = true
		return nil
	}
	updated, _ := m.Update(keyRune('n'))
	m = updated.(model)
	if !m.creating || !strings.Contains(m.View(), "新建项目") {
		t.Fatalf("N did not open an interactive creation form: %#v", m)
	}
	m.field = 1
	m.name.SetValue("新项目")
	m.input.SetValue("名单.txt")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.creating || created != "新项目|名单.txt" {
		t.Fatalf("creation form did not invoke callback: creating=%v callback=%q", m.creating, created)
	}
	m.rows = []projectRow{row}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.message, "详情：项目 A") || !strings.Contains(m.message, "完成 3/8") {
		t.Fatalf("Enter did not display project details: %q", m.message)
	}
	updated, _ = m.Update(keyRune('v'))
	m = updated.(model)
	if !reviewed || !strings.Contains(m.message, "已刷新") {
		t.Fatalf("V did not invoke review: reviewed=%v message=%q", reviewed, m.message)
	}
}

func TestPanelRefreshSchedulesNextTick(t *testing.T) {
	m := model{root: workspace.Workspace{Root: "/definitely-missing"}, cursor: 2}
	updated, command := m.Update(refreshMsg(time.Now()))
	got := updated.(model)
	if command == nil || got.cursor != 0 {
		t.Fatalf("refresh did not reset stale selection or schedule next tick: cursor=%d command=%v", got.cursor, command)
	}
}

func TestCreationFormAcceptsLowercaseQ(t *testing.T) {
	m := newModel(workspace.Workspace{Root: "/definitely-missing"}, nil, nil, nil, nil)
	updated, _ := m.Update(keyRune('n'))
	m = updated.(model)
	updated, _ = m.Update(keyRune('q'))
	m = updated.(model)
	if !m.creating || m.name.Value() != "q" {
		t.Fatalf("q quit or bypassed creation input: creating=%v value=%q", m.creating, m.name.Value())
	}
}
