// Package store persists project state and the cross-project work queue.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/workspace"
)

type ProjectStore struct {
	db *sql.DB
}

func OpenProject(project workspace.Project) (*ProjectStore, error) {
	db, err := sql.Open("sqlite", filepath.ToSlash(project.DBPath())+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &ProjectStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *ProjectStore) Close() error { return s.db.Close() }

func (s *ProjectStore) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS subjects (
 id INTEGER PRIMARY KEY, sequence INTEGER NOT NULL UNIQUE, name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS tasks (
 id INTEGER PRIMARY KEY, subject_id INTEGER NOT NULL REFERENCES subjects(id), source_id TEXT NOT NULL,
 status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, lease_until INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', screenshot_path TEXT NOT NULL DEFAULT '', replaces_task_id INTEGER,
 updated_at INTEGER NOT NULL, UNIQUE(subject_id, source_id)
);
CREATE TABLE IF NOT EXISTS locks (
 name TEXT PRIMARY KEY, holder TEXT NOT NULL, lease_until INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS screenshots (
 id INTEGER PRIMARY KEY, task_id INTEGER NOT NULL REFERENCES tasks(id), path TEXT NOT NULL,
 captured_at INTEGER NOT NULL, replaces_id INTEGER, confirmed INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS tasks_ready ON tasks(status, lease_until, id);
`)
	return err
}

func (s *ProjectStore) Seed(names []string, sources []string) error {
	now := time.Now().Unix()
	transaction, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	var sequenceOffset int
	if err := transaction.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM subjects`).Scan(&sequenceOffset); err != nil {
		return err
	}
	for index, name := range names {
		result, err := transaction.Exec(`INSERT OR IGNORE INTO subjects(sequence, name) VALUES (?, ?)`, sequenceOffset+index+1, name)
		if err != nil {
			return err
		}
		var subjectID int64
		if _, err := result.RowsAffected(); err != nil {
			return err
		}
		if err := transaction.QueryRow(`SELECT id FROM subjects WHERE name = ?`, name).Scan(&subjectID); err != nil {
			return err
		}
		for _, source := range sources {
			if _, err := transaction.Exec(`INSERT OR IGNORE INTO tasks(subject_id, source_id, status, updated_at) VALUES (?, ?, ?, ?)`,
				subjectID, source, domain.Pending, now); err != nil {
				return err
			}
		}
	}
	return transaction.Commit()
}

func (s *ProjectStore) Count() (total, completed, pending, review, failed int, err error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status domain.CheckStatus
		var count int
		if err = rows.Scan(&status, &count); err != nil {
			return
		}
		total += count
		switch status {
		case domain.Found, domain.NotFound:
			completed += count
		case domain.NeedsReview:
			review += count
		case domain.RetryableError, domain.FatalError:
			failed += count
		default:
			pending += count
		}
	}
	err = rows.Err()
	return
}

func (s *ProjectStore) Statuses() ([]domain.CheckStatus, error) {
	rows, err := s.db.Query(`SELECT status FROM tasks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var statuses []domain.CheckStatus
	for rows.Next() {
		var value domain.CheckStatus
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		statuses = append(statuses, value)
	}
	return statuses, rows.Err()
}

func scanTask(row interface{ Scan(...any) error }) (domain.Task, error) {
	var task domain.Task
	var replaces sql.NullInt64
	err := row.Scan(&task.ID, &task.SubjectID, &task.Sequence, &task.Subject, &task.SourceID,
		&task.Status, &task.Attempts, &task.LeaseUntil, &task.LastError, &task.ScreenshotPath, &replaces)
	if replaces.Valid {
		task.ReplacesID = &replaces.Int64
	}
	return task, err
}

const taskColumns = `t.id, t.subject_id, s.sequence, s.name, t.source_id, t.status, t.attempts,
t.lease_until, t.last_error, t.screenshot_path, t.replaces_task_id`

func (s *ProjectStore) List(status domain.CheckStatus) ([]domain.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks t JOIN subjects s ON s.id=t.subject_id`
	args := []any{}
	if status != "" {
		query += ` WHERE t.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY s.sequence, t.source_id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// RecoverExpiredLeases makes a crashed worker's in-flight work eligible again.
func (s *ProjectStore) RecoverExpiredLeases(now time.Time) error {
	_, err := s.db.Exec(`UPDATE tasks SET status=?, lease_until=0, updated_at=?
WHERE status=? AND lease_until < ?`, domain.Pending, now.Unix(), domain.Running, now.Unix())
	return err
}

func (s *ProjectStore) ClaimNext(holder string, lease time.Duration) (*domain.Task, error) {
	now := time.Now()
	if err := s.RecoverExpiredLeases(now); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRow(`SELECT `+taskColumns+` FROM tasks t JOIN subjects s ON s.id=t.subject_id
WHERE t.status=? ORDER BY s.sequence, t.id LIMIT 1`, domain.Pending)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(`UPDATE tasks SET status=?, attempts=attempts+1, lease_until=?, updated_at=?
WHERE id=? AND status=?`, domain.Running, now.Add(lease).Unix(), now.Unix(), task.ID, domain.Pending)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, nil
	}
	task.Status, task.LeaseUntil, task.Attempts = domain.Running, now.Add(lease).Unix(), task.Attempts+1
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *ProjectStore) Complete(taskID int64, status domain.CheckStatus, message, screenshot string) error {
	if err := status.Validate(); err != nil {
		return err
	}
	if status == domain.NotFound || status == domain.Found {
		if screenshot == "" {
			return errors.New("confirmed result requires a screenshot")
		}
	}
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var previous sql.NullInt64
	if screenshot != "" {
		_ = tx.QueryRow(`SELECT id FROM screenshots WHERE task_id=? AND confirmed=1 ORDER BY id DESC LIMIT 1`, taskID).Scan(&previous)
		result, err := tx.Exec(`INSERT INTO screenshots(task_id, path, captured_at, replaces_id) VALUES (?, ?, ?, ?)`,
			taskID, screenshot, now, nullableInt(previous))
		if err != nil {
			return err
		}
		newID, _ := result.LastInsertId()
		if previous.Valid {
			if _, err := tx.Exec(`UPDATE screenshots SET confirmed=0 WHERE id=?`, previous.Int64); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE tasks SET replaces_task_id=? WHERE id=?`, previous.Int64, taskID); err != nil {
				return err
			}
		}
		_ = newID
	}
	_, err = tx.Exec(`UPDATE tasks SET status=?, lease_until=0, last_error=?, screenshot_path=CASE WHEN ?='' THEN screenshot_path ELSE ? END, updated_at=? WHERE id=?`,
		status, message, screenshot, screenshot, now, taskID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

// RetryFailures intentionally excludes fatal errors: those indicate a source
// contract change and require a maintainer rather than repeated traffic.
func (s *ProjectStore) RetryFailures() (int64, error) {
	result, err := s.db.Exec(`UPDATE tasks SET status=?, lease_until=0, last_error='', updated_at=?
WHERE status=?`, domain.Pending, time.Now().Unix(), domain.RetryableError)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *ProjectStore) AcquireLock(name, holder string, lease time.Duration) (bool, error) {
	now := time.Now()
	result, err := s.db.Exec(`INSERT INTO locks(name, holder, lease_until) VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET holder=excluded.holder, lease_until=excluded.lease_until
WHERE locks.lease_until < ? OR locks.holder = excluded.holder`, name, holder, now.Add(lease).Unix(), now.Unix())
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	return count == 1, nil
}

func (s *ProjectStore) ReleaseLock(name, holder string) error {
	_, err := s.db.Exec(`DELETE FROM locks WHERE name=? AND holder=?`, name, holder)
	return err
}

type QueueStore struct{ db *sql.DB }

type QueueItem struct {
	ProjectID   string
	ProjectPath string
	LeaseUntil  int64
	LastError   string
}

func OpenQueue(workspaceRoot string) (*QueueStore, error) {
	path := filepath.Join(workspaceRoot, "_legalscout", "queue.db")
	db, err := sql.Open("sqlite", filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &QueueStore{db: db}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS queue (
 project_id TEXT PRIMARY KEY, project_path TEXT NOT NULL, status TEXT NOT NULL,
 lease_until INTEGER NOT NULL DEFAULT 0, last_started INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS worker_locks (name TEXT PRIMARY KEY, holder TEXT NOT NULL, lease_until INTEGER NOT NULL);
`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	// Existing early Go workspaces did not retain queue errors. SQLite has no
	// ADD COLUMN IF NOT EXISTS, so accept the duplicate-column migration.
	if _, err = db.Exec(`ALTER TABLE queue ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *QueueStore) Close() error { return s.db.Close() }

func (s *QueueStore) Enqueue(project workspace.Project) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO queue(project_id, project_path, status, created_at) VALUES (?, ?, 'queued', ?)
ON CONFLICT(project_id) DO UPDATE SET project_path=excluded.project_path,
status=CASE WHEN queue.status='running' AND queue.lease_until>? THEN queue.status ELSE 'queued' END`,
		project.ID, project.Path, now, now)
	return err
}

func (s *QueueStore) AcquireWorker(holder string, lease time.Duration, slots int) (bool, error) {
	if slots < 1 {
		slots = 1
	}
	if slots > 8 {
		slots = 8
	}
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRow(`SELECT name FROM worker_locks WHERE holder=? ORDER BY name LIMIT 1`, holder).Scan(&current)
	if err == nil {
		if _, err := tx.Exec(`DELETE FROM worker_locks WHERE holder=? AND name<>?`, holder, current); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`UPDATE worker_locks SET lease_until=? WHERE name=? AND holder=?`,
			now.Add(lease).Unix(), current, holder); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for slot := 0; slot < slots; slot++ {
		result, err := tx.Exec(`INSERT INTO worker_locks(name, holder, lease_until) VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET holder=excluded.holder, lease_until=excluded.lease_until
WHERE worker_locks.lease_until < ? OR worker_locks.holder=excluded.holder`,
			fmt.Sprintf("worker:%d", slot), holder, now.Add(lease).Unix(), now.Unix())
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if n == 1 {
			return true, tx.Commit()
		}
	}
	return false, tx.Commit()
}

func (s *QueueStore) ClaimProject(lease time.Duration) (*QueueItem, error) {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE queue SET status='queued', lease_until=0 WHERE status='running' AND lease_until < ?`, now.Unix())
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var item QueueItem
	err = tx.QueryRow(`SELECT project_id, project_path, last_error FROM queue WHERE status='queued'
ORDER BY last_started ASC, created_at ASC LIMIT 1`).Scan(&item.ProjectID, &item.ProjectPath, &item.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.LeaseUntil = now.Add(lease).Unix()
	result, err := tx.Exec(`UPDATE queue SET status='running', lease_until=?, last_started=? WHERE project_id=? AND status='queued'`,
		item.LeaseUntil, now.Unix(), item.ProjectID)
	if err != nil {
		return nil, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *QueueStore) Requeue(projectID string) error {
	_, err := s.db.Exec(`UPDATE queue SET status='queued', lease_until=0 WHERE project_id=? AND status='running'`, projectID)
	return err
}

func (s *QueueStore) Finish(projectID string) error {
	_, err := s.db.Exec(`UPDATE queue SET status='done', lease_until=0 WHERE project_id=? AND status='running'`, projectID)
	return err
}

// Fail retains an actionable queue row for a project that was moved or whose
// database is damaged, allowing other projects to keep running.
func (s *QueueStore) Fail(projectID string, cause error) error {
	if cause == nil {
		cause = errors.New("unknown queue failure")
	}
	_, err := s.db.Exec(`UPDATE queue SET status='failed', lease_until=0, last_error=? WHERE project_id=?`,
		cause.Error(), projectID)
	return err
}

// Remove atomically removes a project from the global scheduling set. Archive
// uses it while holding the project lock so a stale worker cannot recreate the
// old project path after the rename.
func (s *QueueStore) Remove(projectID string) error {
	_, err := s.db.Exec(`DELETE FROM queue WHERE project_id=?`, projectID)
	return err
}

// Cancel stops an active or queued project without deleting its diagnostic
// queue state. A later start command may enqueue it again.
func (s *QueueStore) Cancel(projectID string) error {
	_, err := s.db.Exec(`UPDATE queue SET status='cancelled', lease_until=0 WHERE project_id=?`, projectID)
	return err
}

func (s *QueueStore) IsRunning(projectID string) (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM queue
WHERE project_id=? AND status='running' AND lease_until>=?`, projectID, time.Now().Unix()).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

func (s *QueueStore) ReleaseWorker(holder string) error {
	_, err := s.db.Exec(`DELETE FROM worker_locks WHERE name LIKE 'worker:%' AND holder=?`, holder)
	return err
}
