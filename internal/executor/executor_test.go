package executor_test

import (
	"context"
	"testing"

	"github.com/wangke19/harness-ai/internal/agent"
	"github.com/wangke19/harness-ai/internal/executor"
	"github.com/wangke19/harness-ai/internal/store"
)

type mockAgent struct{ text string }

func (m *mockAgent) Complete(ctx context.Context, prompt string) (string, error) {
	return m.text, nil
}
func (m *mockAgent) CompleteWithTools(ctx context.Context, prompt string, tools []agent.Tool) (string, []agent.ToolCall, error) {
	return m.text, nil, nil
}

func TestExecutor_ReturnsBlockerOnMaxRetries(t *testing.T) {
	task := &store.Task{
		ID:         "gh-5",
		IssueURL:   "https://github.com/owner/repo/issues/5",
		Status:     store.StatusExecuting,
		RetryCount: 3, // already at max
		PlanPath:   "/nonexistent/plan.md",
	}

	a := &mockAgent{text: "done"}
	e := executor.New(a, "/tmp/test-repo", 3)

	err := e.Execute(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for max retries")
	}
	if _, ok := err.(agent.ErrBlocker); !ok {
		t.Errorf("expected ErrBlocker, got %T: %v", err, err)
	}
}
