package watcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/wangke19/harness-ai/internal/store"
)

// Issue is a normalized issue from any source.
type Issue struct {
	ID     string
	URL    string
	Title  string
	Body   string
	Source string // "github" or "jira"
}

// IssueEvent is emitted on the channel when a new issue is found.
type IssueEvent struct {
	Issue Issue
}

// IssueSource is implemented by GitHub and Jira watchers.
type IssueSource interface {
	FetchNew(ctx context.Context, since time.Time) ([]Issue, error)
}

// Watcher polls IssueSource(s) and emits IssueEvents for unseen issues.
type Watcher struct {
	store    *store.Store
	events   chan<- IssueEvent
	interval time.Duration
	sources  []IssueSource
}

func New(s *store.Store, events chan<- IssueEvent, interval time.Duration, sources ...IssueSource) *Watcher {
	return &Watcher{store: s, events: events, interval: interval, sources: sources}
}

func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	since := time.Now().Add(-24 * time.Hour)

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			w.poll(ctx, since)
			since = t
		}
	}
}

func (w *Watcher) poll(ctx context.Context, since time.Time) {
	for _, src := range w.sources {
		issues, err := src.FetchNew(ctx, since)
		if err != nil {
			slog.Error("watcher poll error", "error", err)
			continue
		}
		for _, issue := range issues {
			if w.store.HasSeen(ctx, issue.URL) {
				continue
			}
			if err := w.store.MarkSeen(ctx, issue.URL); err != nil {
				slog.Error("mark seen error", "url", issue.URL, "error", err)
				continue
			}
			w.events <- IssueEvent{Issue: issue}
		}
	}
}
