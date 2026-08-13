package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

func testProject(t *testing.T) (workspace.Workspace, workspace.Project) {
	t.Helper()
	root := filepath.Join(".", ".test-store-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	project, err := ws.Create("存储测试")
	if err != nil {
		t.Fatal(err)
	}
	return ws, project
}

func TestExpiredLeaseIsRecoveredAndRetrySkipsConfirmed(t *testing.T) {
	_, project := testProject(t)
	state, err := OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.Seed([]string{"张三", "李四", "王五"}, []string{"csrc"}); err != nil {
		t.Fatal(err)
	}
	task, err := state.ClaimNext("test", -time.Second)
	if err != nil || task == nil {
		t.Fatalf("claim: %v %#v", err, task)
	}
	if err := state.RecoverExpiredLeases(time.Now()); err != nil {
		t.Fatal(err)
	}
	recovered, err := state.ClaimNext("test", time.Second)
	if err != nil || recovered == nil || recovered.ID != task.ID {
		t.Fatalf("recovered = %#v, err = %v", recovered, err)
	}
	if err := state.Complete(recovered.ID, domain.Found, "", "截图/001-张三/a.png"); err != nil {
		t.Fatal(err)
	}
	if err := state.Complete(recovered.ID, domain.Found, "", "截图/001-张三/b.png"); err != nil {
		t.Fatal(err)
	}
	var versions, current, replacements int
	if err := state.db.QueryRow(`SELECT COUNT(*), SUM(confirmed), COUNT(replaces_id) FROM screenshots WHERE task_id=?`, recovered.ID).
		Scan(&versions, &current, &replacements); err != nil {
		t.Fatal(err)
	}
	if versions != 2 || current != 1 || replacements != 1 {
		t.Fatalf("screenshot replacement chain = versions:%d current:%d replacements:%d", versions, current, replacements)
	}
	next, err := state.ClaimNext("test", time.Second)
	if err != nil || next == nil {
		t.Fatalf("next task: %v %#v", err, next)
	}
	if err := state.Complete(next.ID, domain.RetryableError, "timeout", ""); err != nil {
		t.Fatal(err)
	}
	fatal, err := state.ClaimNext("test", time.Second)
	if err != nil || fatal == nil {
		t.Fatalf("fatal task: %v %#v", err, fatal)
	}
	if err := state.Complete(fatal.ID, domain.FatalError, "source contract changed", ""); err != nil {
		t.Fatal(err)
	}
	count, err := state.RetryFailures()
	if err != nil || count != 1 {
		t.Fatalf("retry count = %d, err = %v", count, err)
	}
	tasks, err := state.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range tasks {
		if current.ID == recovered.ID && current.Status != domain.Found {
			t.Fatalf("confirmed task was reset: %#v", current)
		}
		if current.ID == fatal.ID && current.Status != domain.FatalError {
			t.Fatalf("fatal task was reset: %#v", current)
		}
	}
}

func TestQueueRecoversLeaseAndSchedulesFairly(t *testing.T) {
	ws, first := testProject(t)
	second, err := ws.Create("第二项目")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := OpenQueue(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.Enqueue(first); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(second); err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.ClaimProject(time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	if err := queue.Requeue(claimed.ProjectID); err != nil {
		t.Fatal(err)
	}
	next, err := queue.ClaimProject(time.Second)
	if err != nil || next == nil {
		t.Fatalf("next: %v %#v", err, next)
	}
	if next.ProjectID == claimed.ProjectID {
		t.Fatalf("queue did not round-robin: %s", next.ProjectID)
	}
}

func TestQueueSupportsDistinctWorkersFailureCancellationAndRemoval(t *testing.T) {
	ws, project := testProject(t)
	queue, err := OpenQueue(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	first, err := queue.AcquireWorker("holder-one", time.Minute, 2)
	if err != nil || !first {
		t.Fatalf("first holder = %v, %v", first, err)
	}
	second, err := queue.AcquireWorker("holder-two", time.Minute, 2)
	if err != nil || !second {
		t.Fatalf("second holder = %v, %v", second, err)
	}
	third, err := queue.AcquireWorker("holder-three", time.Minute, 2)
	if err != nil || third {
		t.Fatalf("third holder bypassed global slot limit: %v, %v", third, err)
	}
	if err := queue.ReleaseWorker("holder-one"); err != nil {
		t.Fatal(err)
	}
	renewed, err := queue.AcquireWorker("holder-two", time.Minute, 2)
	if err != nil || !renewed {
		t.Fatalf("renew second holder = %v, %v", renewed, err)
	}
	var heldByTwo int
	if err := queue.db.QueryRow(`SELECT COUNT(*) FROM worker_locks WHERE holder='holder-two'`).Scan(&heldByTwo); err != nil {
		t.Fatal(err)
	}
	if heldByTwo != 1 {
		t.Fatalf("releasing one holder removed another holder: %d", heldByTwo)
	}
	var secondSlot string
	if err := queue.db.QueryRow(`SELECT name FROM worker_locks WHERE holder='holder-two'`).Scan(&secondSlot); err != nil {
		t.Fatal(err)
	}
	if secondSlot != "worker:1" {
		t.Fatalf("renewal migrated holder into another slot: %s", secondSlot)
	}
	claimed, err := queue.ClaimProject(time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if err := queue.Fail(project.ID, errors.New("project metadata missing")); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	if err := queue.db.QueryRow(`SELECT status, last_error FROM queue WHERE project_id=?`, project.ID).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || lastError == "" {
		t.Fatalf("failed queue state = %q/%q", status, lastError)
	}
	if err := queue.Cancel(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := queue.Remove(project.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := queue.db.QueryRow(`SELECT COUNT(*) FROM queue WHERE project_id=?`, project.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("removed queue row remains: %d", remaining)
	}
}

func TestIsRunningIgnoresExpiredLease(t *testing.T) {
	ws, project := testProject(t)
	queue, err := OpenQueue(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.Enqueue(project); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.db.Exec(`UPDATE queue SET status='running', lease_until=? WHERE project_id=?`,
		time.Now().Add(-time.Second).Unix(), project.ID); err != nil {
		t.Fatal(err)
	}
	running, err := queue.IsRunning(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Fatal("expired queue lease was reported as active")
	}
}
