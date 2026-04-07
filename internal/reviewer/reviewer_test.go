package reviewer_test

import (
	"context"
	"testing"

	"github.com/wangke19/harness-system/internal/agent"
	"github.com/wangke19/harness-system/internal/reviewer"
	"github.com/wangke19/harness-system/internal/store"
)

type mockAgent struct{ decision string }

func (m *mockAgent) Complete(ctx context.Context, prompt string) (string, error) {
	return m.decision, nil
}
func (m *mockAgent) CompleteWithTools(ctx context.Context, prompt string, tools []agent.Tool) (string, []agent.ToolCall, error) {
	return m.decision, nil, nil
}

func TestReviewer_ApproveDecision(t *testing.T) {
	task := &store.Task{ID: "gh-3", PRNumber: 42, Status: store.StatusReviewing}
	a := &mockAgent{decision: "APPROVE"}
	r := reviewer.New(a, nil)

	decision, err := r.Review(context.Background(), task, "+some diff")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if decision != reviewer.DecisionApprove {
		t.Errorf("got %q, want Approve", decision)
	}
}

func TestReviewer_RequestChangesDecision(t *testing.T) {
	task := &store.Task{ID: "gh-4", PRNumber: 43, Status: store.StatusReviewing}
	a := &mockAgent{decision: "REQUEST_CHANGES: missing error handling in handler"}
	r := reviewer.New(a, nil)

	decision, err := r.Review(context.Background(), task, "+some diff")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if decision != reviewer.DecisionRequestChanges {
		t.Errorf("got %q, want RequestChanges", decision)
	}
}
