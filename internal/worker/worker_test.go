package worker

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Duan-JM/LegalScout/internal/browser"
	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/sources"
	"github.com/Duan-JM/LegalScout/internal/store"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

type fakeRunner struct {
	result browser.Result
	err    error
}

func (r fakeRunner) Run(context.Context, sources.Adapter, string, bool) (browser.Result, error) {
	return r.result, r.err
}
func (r fakeRunner) Preflight(context.Context, sources.Adapter) error { return nil }

type recordingRunner struct {
	result       browser.Result
	runErr       error
	preflightErr error
	runs         int
	preflights   int
}

type delayedRunner struct {
	delay  time.Duration
	result browser.Result
}

type blockingRunner struct {
	started chan struct{}
}

func (r blockingRunner) Run(ctx context.Context, _ sources.Adapter, _ string, _ bool) (browser.Result, error) {
	close(r.started)
	<-ctx.Done()
	return browser.Result{Status: domain.RetryableError}, ctx.Err()
}

func (r blockingRunner) Preflight(context.Context, sources.Adapter) error { return nil }

func (r delayedRunner) Run(context.Context, sources.Adapter, string, bool) (browser.Result, error) {
	time.Sleep(r.delay)
	return r.result, nil
}

func (r delayedRunner) Preflight(context.Context, sources.Adapter) error { return nil }

func (r *recordingRunner) Run(context.Context, sources.Adapter, string, bool) (browser.Result, error) {
	r.runs++
	return r.result, r.runErr
}
func (r *recordingRunner) Preflight(context.Context, sources.Adapter) error {
	r.preflights++
	return r.preflightErr
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	image := image.NewRGBA(image.Rect(0, 0, 50, 50))
	for x := 0; x < 50; x++ {
		for y := 0; y < 50; y++ {
			image.Set(x, y, color.White)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, image); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func setup(t *testing.T, runner browser.Runner) (*Engine, workspace.Project, *store.ProjectStore) {
	t.Helper()
	rootPath := filepath.Join(".", ".test-worker-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(rootPath) })
	root, err := workspace.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := root.Create("执行测试")
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Seed([]string{"张三"}, []string{"csrc"}); err != nil {
		t.Fatal(err)
	}
	queue, err := store.OpenQueue(root.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close(); _ = state.Close() })
	if err := queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	return New(root, queue, runner), project, state
}

func TestWorkerDoesNotDisguiseRunnerErrorAsNotFound(t *testing.T) {
	engine, _, state := setup(t, fakeRunner{result: browser.Result{Status: domain.RetryableError}, err: os.ErrDeadlineExceeded})
	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Status != domain.RetryableError {
		t.Fatalf("error status=%s, want retryable_error", tasks[0].Status)
	}
}

func TestWorkerWritesConfirmedScreenshot(t *testing.T) {
	engine, project, state := setup(t, fakeRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}})
	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Status != domain.NotFound || tasks[0].ScreenshotPath == "" {
		t.Fatalf("task = %#v", tasks[0])
	}
	if _, err := os.Stat(filepath.Join(project.Path, tasks[0].ScreenshotPath)); err != nil {
		t.Fatalf("screenshot absent: %v", err)
	}
}

func TestWorkerPreservesStructuredErrorOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		result browser.Result
		want   domain.CheckStatus
	}{
		{"fatal", browser.Result{Status: domain.FatalError}, domain.FatalError},
		{"needs review", browser.Result{Status: domain.NeedsReview}, domain.NeedsReview},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, _, state := setup(t, fakeRunner{result: test.result, err: errors.New("network timeout")})
			if _, err := engine.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			tasks, err := state.List("")
			if err != nil {
				t.Fatal(err)
			}
			if tasks[0].Status != test.want {
				t.Fatalf("status=%s, want %s", tasks[0].Status, test.want)
			}
		})
	}
}

func TestWorkerPreflightCachesAndBlocksRun(t *testing.T) {
	runner := &recordingRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}}
	engine, _, state := setup(t, runner)
	if err := state.Seed([]string{"李四"}, []string{"csrc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.preflights != 1 || runner.runs != 2 {
		t.Fatalf("preflight=%d runs=%d, want 1/2", runner.preflights, runner.runs)
	}

	blocked := &recordingRunner{preflightErr: &browser.PreflightError{Status: domain.FatalError, Err: errors.New("contract changed")}}
	engine, _, state = setup(t, blocked)
	if _, err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.runs != 0 || tasks[0].Status != domain.FatalError {
		t.Fatalf("runs=%d status=%s, want no run/fatal", blocked.runs, tasks[0].Status)
	}
}

func TestReviewCountsOnlyConfirmedResults(t *testing.T) {
	runner := &recordingRunner{result: browser.Result{Status: domain.NeedsReview}}
	engine, project, state := setup(t, runner)
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Complete(tasks[0].ID, domain.NeedsReview, "验证码", ""); err != nil {
		t.Fatal(err)
	}
	count, err := engine.Review(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unconfirmed review count=%d, want 0", count)
	}
	runner.result = browser.Result{Status: domain.Found, Screenshot: testPNG(t)}
	count, err = engine.Review(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("confirmed review count=%d, want 1", count)
	}
}

func TestWorkerRemovesScreenshotWhenCompletionFails(t *testing.T) {
	engine, project, _ := setup(t, fakeRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}})
	triggerDB, err := sql.Open("sqlite", project.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	_, err = triggerDB.Exec(`CREATE TRIGGER fail_confirmed_completion BEFORE UPDATE ON tasks
WHEN NEW.status IN ('found', 'not_found') BEGIN SELECT RAISE(FAIL, 'forced completion failure'); END;`)
	if closeErr := triggerDB.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunOnce(context.Background()); err == nil {
		t.Fatal("completion failure was not returned")
	}
	var pngs []string
	if err := filepath.WalkDir(project.ScreenshotsPath(), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".png" {
			pngs = append(pngs, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(pngs) != 0 {
		t.Fatalf("screenshot directory retained rolled-back file: %#v", pngs)
	}
}

func TestRunUntilIdleWaitsForBriefQueueGap(t *testing.T) {
	t.Setenv("LEGALSCOUT_MAX_CONCURRENCY", "1")
	runner := &recordingRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}}
	engine, project, _ := setup(t, runner)
	item, err := engine.Queue.ClaimProject(time.Minute)
	if err != nil || item == nil {
		t.Fatalf("prepare empty queue: %#v, %v", item, err)
	}

	if err := engine.Queue.Finish(project.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- engine.RunUntilIdle(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	if err := engine.Queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runner.runs != 1 {
		t.Fatalf("worker exited at the transient empty queue: runs=%d", runner.runs)
	}
}

func TestWorkerWaitsForCrashedSlotLeaseExpiry(t *testing.T) {
	t.Setenv("LEGALSCOUT_MAX_CONCURRENCY", "1")
	runner := &recordingRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}}
	engine, _, _ := setup(t, runner)
	engine.Lease = 1200 * time.Millisecond
	ok, err := engine.Queue.AcquireWorker("crashed-holder", engine.Lease, 1)
	if err != nil || !ok {
		t.Fatalf("occupy worker slot: %v, %v", ok, err)
	}

	started := time.Now()
	if err := engine.RunUntilIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < engine.Lease || runner.runs != 1 {
		db, err := sql.Open("sqlite", filepath.Join(engine.Workspace.Root, "_legalscout", "queue.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var status, holder string
		var lease int64
		_ = db.QueryRow(`SELECT status FROM queue LIMIT 1`).Scan(&status)
		_ = db.QueryRow(`SELECT holder, lease_until FROM worker_locks LIMIT 1`).Scan(&holder, &lease)
		t.Fatalf("replacement worker did not wait/recover: elapsed=%v runs=%d queue=%s lock=%s/%d",
			time.Since(started), runner.runs, status, holder, lease)
	}
}

func TestWorkerDropsProjectMarkedForArchiving(t *testing.T) {
	runner := &recordingRunner{result: browser.Result{Status: domain.NotFound, Screenshot: testPNG(t)}}
	engine, project, _ := setup(t, runner)
	if err := os.WriteFile(project.ArchivingPath(), []byte("archiving"), 0o600); err != nil {
		t.Fatal(err)
	}
	didWork, err := engine.RunOnce(context.Background())
	if err != nil || !didWork {
		t.Fatalf("archiving project handling = %v, %v", didWork, err)
	}
	if runner.runs != 0 {
		t.Fatal("worker executed a project marked for archiving")
	}
	item, err := engine.Queue.ClaimProject(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if item != nil {
		t.Fatalf("archiving project remained queued: %#v", item)
	}
}

func TestReviewRenewsProjectLockDuringLongManualTask(t *testing.T) {
	runner := delayedRunner{delay: 140 * time.Millisecond, result: browser.Result{Status: domain.Found, Screenshot: testPNG(t)}}
	engine, project, state := setup(t, runner)
	engine.Lease = 60 * time.Millisecond
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Complete(tasks[0].ID, domain.NeedsReview, "验证码", ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := engine.Review(context.Background(), project)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	other, err := store.OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	acquired, err := other.AcquireLock("project", "competing-review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("competing review acquired an actively renewed project lock")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	acquired, err = other.AcquireLock("project", "competing-review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("review left a renewed project lock behind")
	}
}

func TestReviewCancelsTaskWithoutWritingAfterLockLoss(t *testing.T) {
	runner := blockingRunner{started: make(chan struct{})}
	engine, project, state := setup(t, runner)
	engine.Lease = 60 * time.Millisecond
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Complete(tasks[0].ID, domain.NeedsReview, "验证码", ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := engine.Review(context.Background(), project)
		done <- err
	}()
	<-runner.started
	other, err := store.OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.ReleaseLock("project", engine.Holder); err != nil {
		t.Fatal(err)
	}
	acquired, err := other.AcquireLock("project", "competing-review", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("competing lock = %v, %v", acquired, err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("review succeeded after losing its project lock")
		}
	case <-time.After(time.Second):
		t.Fatal("review did not cancel after losing its project lock")
	}
	updated, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range updated {
		if task.ID == tasks[0].ID && task.Status != domain.NeedsReview {
			t.Fatalf("lock-lost review wrote task status %s", task.Status)
		}
	}
}
