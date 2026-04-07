package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusPlanning  TaskStatus = "planning"
	StatusExecuting TaskStatus = "executing"
	StatusReviewing TaskStatus = "reviewing"
	StatusMerging   TaskStatus = "merging"
	StatusBlocked   TaskStatus = "blocked"
	StatusDone      TaskStatus = "done"
	StatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID          string
	IssueURL    string
	Status      TaskStatus
	BlockReason string
	PlanPath    string
	PRNumber    int
	RetryCount  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store struct {
	db *sql.DB
}

func New(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id           TEXT PRIMARY KEY,
			issue_url    TEXT NOT NULL,
			status       TEXT NOT NULL,
			block_reason TEXT NOT NULL DEFAULT '',
			plan_path    TEXT NOT NULL DEFAULT '',
			pr_number    INTEGER NOT NULL DEFAULT 0,
			retry_count  INTEGER NOT NULL DEFAULT 0,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS seen_issues (
			url TEXT PRIMARY KEY,
			seen_at DATETIME NOT NULL
		);
	`)
	return err
}

func (s *Store) CreateTask(ctx context.Context, t *Task) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.IssueURL, t.Status, t.BlockReason, t.PlanPath, t.PRNumber, t.RetryCount, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status TaskStatus, blockReason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, block_reason = ?, updated_at = ? WHERE id = ?`,
		status, blockReason, time.Now().UTC(), id,
	)
	return err
}

func (s *Store) UpdateTask(ctx context.Context, t *Task) error {
	t.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status=?, block_reason=?, plan_path=?, pr_number=?, retry_count=?, updated_at=? WHERE id=?`,
		t.Status, t.BlockReason, t.PlanPath, t.PRNumber, t.RetryCount, t.UpdatedAt, t.ID,
	)
	return err
}

func (s *Store) ListActiveTasks(ctx context.Context) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at
		 FROM tasks WHERE status NOT IN ('done', 'failed')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) HasSeen(ctx context.Context, url string) bool {
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen_issues WHERE url = ?`, url).Scan(&count)
	return count > 0
}

func (s *Store) MarkSeen(ctx context.Context, url string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO seen_issues (url, seen_at) VALUES (?, ?)`, url, time.Now().UTC())
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*Task, error) {
	var t Task
	err := s.Scan(&t.ID, &t.IssueURL, &t.Status, &t.BlockReason, &t.PlanPath, &t.PRNumber, &t.RetryCount, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
