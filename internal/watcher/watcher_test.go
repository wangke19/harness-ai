package watcher_test

import (
	"context"
	"testing"
	"time"

	"github.com/wangke19/harness-system/internal/store"
	"github.com/wangke19/harness-system/internal/watcher"
)

type mockSource struct {
	issues []watcher.Issue
}

func (m *mockSource) FetchNew(ctx context.Context, since time.Time) ([]watcher.Issue, error) {
	return m.issues, nil
}

func TestWatcher_EmitsNewIssues(t *testing.T) {
	s, _ := store.New(":memory:")
	defer s.Close()

	src := &mockSource{issues: []watcher.Issue{
		{ID: "gh-1", URL: "https://github.com/owner/repo/issues/1", Title: "Fix bug", Source: "github"},
	}}

	events := make(chan watcher.IssueEvent, 10)
	w := watcher.New(s, events, 50*time.Millisecond, src)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go w.Run(ctx)

	select {
	case ev := <-events:
		if ev.Issue.ID != "gh-1" {
			t.Errorf("got issue ID %q, want %q", ev.Issue.ID, "gh-1")
		}
	case <-ctx.Done():
		t.Fatal("timeout: no issue event received")
	}
}

func TestWatcher_DeduplicatesSeenIssues(t *testing.T) {
	s, _ := store.New(":memory:")
	defer s.Close()

	src := &mockSource{issues: []watcher.Issue{
		{ID: "gh-2", URL: "https://github.com/owner/repo/issues/2", Title: "Dup", Source: "github"},
	}}

	events := make(chan watcher.IssueEvent, 10)
	w := watcher.New(s, events, 50*time.Millisecond, src)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go w.Run(ctx)

	count := 0
	timeout := time.After(250 * time.Millisecond)
	for {
		select {
		case <-events:
			count++
		case <-timeout:
			if count != 1 {
				t.Errorf("got %d events for same issue, want exactly 1", count)
			}
			return
		}
	}
}
