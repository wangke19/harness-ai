package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/wangke19/harness-system/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := &store.Task{
		ID:       "test-1",
		IssueURL: "https://github.com/owner/repo/issues/1",
		Status:   store.StatusPending,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.IssueURL != task.IssueURL {
		t.Errorf("IssueURL: got %q want %q", got.IssueURL, task.IssueURL)
	}
	if got.Status != store.StatusPending {
		t.Errorf("Status: got %q want %q", got.Status, store.StatusPending)
	}
}

func TestUpdateStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := &store.Task{ID: "t2", IssueURL: "https://github.com/owner/repo/issues/2", Status: store.StatusPending}
	_ = s.CreateTask(ctx, task)

	if err := s.UpdateStatus(ctx, "t2", store.StatusPlanning, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := s.GetTask(ctx, "t2")
	if got.Status != store.StatusPlanning {
		t.Errorf("Status: got %q want %q", got.Status, store.StatusPlanning)
	}
}

func TestListActiveTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, status := range []store.TaskStatus{store.StatusPending, store.StatusPlanning, store.StatusDone} {
		_ = s.CreateTask(ctx, &store.Task{
			ID:       fmt.Sprintf("t%d", i),
			IssueURL: fmt.Sprintf("https://github.com/owner/repo/issues/%d", i),
			Status:   status,
		})
	}

	tasks, err := s.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	if len(tasks) != 2 { // Pending + Planning, not Done
		t.Errorf("got %d active tasks, want 2", len(tasks))
	}
}

func TestSeenIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	url := "https://github.com/owner/repo/issues/99"
	if s.HasSeen(ctx, url) {
		t.Error("should not have seen issue yet")
	}
	_ = s.MarkSeen(ctx, url)
	if !s.HasSeen(ctx, url) {
		t.Error("should have seen issue after marking")
	}
}
