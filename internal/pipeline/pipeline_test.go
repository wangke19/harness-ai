package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/wangke19/harness-system/internal/pipeline"
	"github.com/wangke19/harness-system/internal/store"
	"github.com/wangke19/harness-system/internal/watcher"
)

type noopHandler struct{ called chan string }

func (h *noopHandler) Handle(ctx context.Context, task *store.Task) error {
	h.called <- task.ID
	return nil
}

func TestPipeline_ProcessesEvent(t *testing.T) {
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()

	handler := &noopHandler{called: make(chan string, 1)}
	events := make(chan watcher.IssueEvent, 1)
	p := pipeline.New(s, events, 2, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go p.Run(ctx)

	events <- watcher.IssueEvent{Issue: watcher.Issue{
		ID: "gh-10", URL: "https://github.com/owner/repo/issues/10", Title: "Test issue",
	}}

	select {
	case id := <-handler.called:
		if id != "gh-10" {
			t.Errorf("got task ID %q, want gh-10", id)
		}
	case <-ctx.Done():
		t.Fatal("timeout: handler not called")
	}
}
