package planner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wangke19/harness-system/internal/agent"
	"github.com/wangke19/harness-system/internal/planner"
	"github.com/wangke19/harness-system/internal/store"
)

type mockAgent struct {
	planText  string
	toolCalls []agent.ToolCall
}

func (m *mockAgent) Complete(ctx context.Context, prompt string) (string, error) {
	return m.planText, nil
}

func (m *mockAgent) CompleteWithTools(ctx context.Context, prompt string, tools []agent.Tool) (string, []agent.ToolCall, error) {
	return m.planText, m.toolCalls, nil
}

func TestPlanner_WritesPlan(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "exec-plans")

	task := &store.Task{
		ID:       "gh-1",
		IssueURL: "https://github.com/owner/repo/issues/1",
		Status:   store.StatusPlanning,
	}

	a := &mockAgent{planText: "## Plan\n\nStep 1: do the thing."}
	p := planner.New(a, planDir)

	planPath, err := p.Plan(context.Background(), task, "Fix the bug in auth.go\n\nDetails: ...")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) == "" {
		t.Error("plan file is empty")
	}
	if task.PlanPath != planPath {
		t.Errorf("task.PlanPath not updated: got %q", task.PlanPath)
	}
}
