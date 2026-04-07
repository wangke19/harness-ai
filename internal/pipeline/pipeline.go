package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wangke19/harness-system/internal/store"
	"github.com/wangke19/harness-system/internal/watcher"
)

// Handler processes a single task. Implemented by the orchestrator that
// chains Planner → Executor → Reviewer → Merger.
type Handler interface {
	Handle(ctx context.Context, task *store.Task) error
}

// Pipeline receives IssueEvents, creates Tasks, and dispatches them to
// a worker pool.
type Pipeline struct {
	store   *store.Store
	events  <-chan watcher.IssueEvent
	workers int
	handler Handler
}

func New(s *store.Store, events <-chan watcher.IssueEvent, workers int, handler Handler) *Pipeline {
	return &Pipeline{store: s, events: events, workers: workers, handler: handler}
}

// Run starts the worker pool and the event dispatch loop.
// It also re-queues active tasks surviving a restart.
func (p *Pipeline) Run(ctx context.Context) {
	work := make(chan *store.Task, p.workers*2)

	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range work {
				if err := p.handler.Handle(ctx, task); err != nil {
					slog.Error("task handler error", "task_id", task.ID, "error", err)
				}
			}
		}()
	}

	// Re-queue tasks that survived restart.
	p.requeueActive(ctx, work)

	// Dispatch new events.
	for {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return
		case ev := <-p.events:
			task := &store.Task{
				ID:       ev.Issue.ID,
				IssueURL: ev.Issue.URL,
				Status:   store.StatusPending,
			}
			if err := p.store.CreateTask(ctx, task); err != nil {
				slog.Error("create task error", "issue_url", ev.Issue.URL, "error", err)
				continue
			}
			work <- task
		}
	}
}

func (p *Pipeline) requeueActive(ctx context.Context, work chan<- *store.Task) {
	tasks, err := p.store.ListActiveTasks(ctx)
	if err != nil {
		slog.Error("list active tasks error", "error", err)
		return
	}
	for _, t := range tasks {
		slog.Info("re-queuing active task", "task_id", t.ID, "status", t.Status)
		work <- t
	}
}

// Transition updates a task's status in the store and logs the change.
func Transition(ctx context.Context, s *store.Store, task *store.Task, status store.TaskStatus, reason string) error {
	task.Status = status
	task.BlockReason = reason
	task.UpdatedAt = time.Now()
	if err := s.UpdateStatus(ctx, task.ID, status, reason); err != nil {
		return fmt.Errorf("transition %s→%s: %w", task.Status, status, err)
	}
	slog.Info("task transition", "task_id", task.ID, "status", status, "reason", reason)
	return nil
}
