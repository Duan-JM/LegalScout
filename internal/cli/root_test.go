package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Duan-JM/LegalScout/internal/browser"
	"github.com/Duan-JM/LegalScout/internal/sources"
	"github.com/Duan-JM/LegalScout/internal/store"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

type smokeRunner struct{}

func (smokeRunner) Run(context.Context, sources.Adapter, string, bool) (browser.Result, error) {
	return browser.Result{}, nil
}
func (smokeRunner) Preflight(context.Context, sources.Adapter) error { return nil }

func testDependencies(rootFn func() (workspace.Workspace, error), output *bytes.Buffer) Dependencies {
	return Dependencies{
		Workspace: rootFn, Runner: smokeRunner{}, StartWorker: func(string) error { return nil },
		OpenPath: func(string) error { return nil }, Output: output,
	}
}

func TestNewAndStatusSmokeWithoutBrowser(t *testing.T) {
	rootPath := filepath.Join(".", ".test-cli-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	input := filepath.Join(rootPath, "名单.txt")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("张三\n李四\n张三\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFn := func() (workspace.Workspace, error) { return workspace.Open(rootPath) }
	var output bytes.Buffer
	command := NewRoot(testDependencies(rootFn, &output))
	command.SetArgs([]string{"new", "恒星科技 IPO", "--input", input, "--checklist", "securities"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "导入 2 个对象、8 项任务") {
		t.Fatalf("new output = %q", output.String())
	}
	output.Reset()
	command = NewRoot(testDependencies(rootFn, &output))
	command.SetArgs([]string{"status"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "完成 0/8") {
		t.Fatalf("status output = %q", output.String())
	}
}

type failingPreflightRunner struct{ smokeRunner }

func (failingPreflightRunner) Preflight(context.Context, sources.Adapter) error {
	return os.ErrDeadlineExceeded
}

func TestDoctorReportsEveryFailureAndReturnsError(t *testing.T) {
	rootPath := filepath.Join(".", ".test-doctor-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	var output bytes.Buffer
	deps := testDependencies(func() (workspace.Workspace, error) { return workspace.Open(rootPath) }, &output)
	deps.Runner = failingPreflightRunner{}
	command := NewRoot(deps)
	command.SetArgs([]string{"doctor"})
	if err := command.Execute(); err == nil {
		t.Fatal("doctor succeeded despite preflight failures")
	}
	if count := strings.Count(output.String(), "预检失败"); count != len(sources.Registry()) {
		t.Fatalf("doctor did not report all source results (%d): %s", count, output.String())
	}
}

func TestNewCleansSpecificPartialProjectOnCopyFailure(t *testing.T) {
	rootPath := filepath.Join(".", ".test-new-cleanup-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// A no-extension list is valid input, while its filename collides with the
	// project's delivery directory, reliably making only the copy step fail.
	input := filepath.Join(rootPath, "截图")
	if err := os.WriteFile(input, []byte("张三\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFn := func() (workspace.Workspace, error) { return workspace.Open(rootPath) }
	var output bytes.Buffer
	command := NewRoot(testDependencies(rootFn, &output))
	command.SetArgs([]string{"new", "半成品", "--input", input})
	if err := command.Execute(); err == nil {
		t.Fatal("new succeeded despite input copy collision")
	}
	root, err := rootFn()
	if err != nil {
		t.Fatal(err)
	}
	projects, err := root.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("partial project was retained: %#v", projects)
	}
}

func TestArchiveRemovesQueueBeforeRenameAndRejectsActiveWorker(t *testing.T) {
	rootPath := filepath.Join(".", ".test-archive-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	root, err := workspace.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}

	project, err := root.Create("待归档")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.ClaimProject(time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim active queue: %#v %v", claimed, err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	rootFn := func() (workspace.Workspace, error) { return root, nil }
	var output bytes.Buffer
	command := NewRoot(testDependencies(rootFn, &output))
	command.SetArgs([]string{"archive", project.Slug})
	if err := command.Execute(); err == nil {
		t.Fatal("archive accepted an active worker")
	}
	if _, err := os.Stat(project.Path); err != nil {
		t.Fatalf("active project moved: %v", err)
	}

	queue, err = store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Cancel(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	command = NewRoot(testDependencies(rootFn, &output))
	command.SetArgs([]string{"archive", project.Slug})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(project.Path); !os.IsNotExist(err) {
		t.Fatalf("archived project still at original path: %v", err)
	}
	queue, err = store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	claimed, err = queue.ClaimProject(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil && claimed.ProjectID == project.ID {
		t.Fatalf("archive retained project in queue: %#v", claimed)
	}
}

func TestEnqueueRejectsArchivingTombstone(t *testing.T) {
	rootPath := filepath.Join(".", ".test-archive-tombstone-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	root, err := workspace.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := root.Create("归档竞态")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project.ArchivingPath(), []byte("archiving"), 0o600); err != nil {
		t.Fatal(err)
	}
	launched := false
	err = enqueue(project, root, func(string) error {
		launched = true
		return nil
	})
	if err == nil || launched {
		t.Fatalf("archiving project enqueue = %v, launched=%v", err, launched)
	}
}

func TestArchiveFailureKeepsQueueAndCleansTombstone(t *testing.T) {
	rootPath := filepath.Join(".", ".test-archive-failure-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	root, err := workspace.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := root.Create("归档失败")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Root, "_archive"), []byte("collision"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFn := func() (workspace.Workspace, error) { return root, nil }
	command := NewRoot(testDependencies(rootFn, &bytes.Buffer{}))
	command.SetArgs([]string{"archive", project.Slug})
	if err := command.Execute(); err == nil {
		t.Fatal("archive unexpectedly succeeded with invalid archive root")
	}
	if project.IsArchiving() {
		t.Fatal("failed archive retained tombstone")
	}
	queue, err = store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	item, err := queue.ClaimProject(time.Minute)
	if err != nil || item == nil || item.ProjectID != project.ID {
		t.Fatalf("failed archive lost queue entry: %#v, %v", item, err)
	}
}

func TestArchiveTargetHandlesLongSlugs(t *testing.T) {
	root := t.TempDir()
	target, err := archiveTarget(root, strings.Repeat("长", 120), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(filepath.Base(target)) > 255 {
		t.Fatalf("archive target exceeds filename limit: %d", len(filepath.Base(target)))
	}
}
