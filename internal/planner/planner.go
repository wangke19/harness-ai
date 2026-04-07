package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangke19/harness-system/internal/agent"
	"github.com/wangke19/harness-system/internal/store"
)

// Planner runs the planning agent loop: issue body → exec plan markdown.
type Planner struct {
	agent   agent.Agent
	planDir string
}

func New(a agent.Agent, planDir string) *Planner {
	return &Planner{agent: a, planDir: planDir}
}

// Plan runs the planner agent for the given task and issue body.
// Writes the plan to exec-plans/<task-id>.md and updates task.PlanPath.
func (p *Planner) Plan(ctx context.Context, task *store.Task, issueBody string) (string, error) {
	tools := plannerTools()
	prompt := buildPlannerPrompt(task, issueBody)

	text, calls, err := p.agent.CompleteWithTools(ctx, prompt, tools)
	if err != nil {
		return "", agent.ErrRetryable{Reason: fmt.Sprintf("planner LLM error: %v", err)}
	}

	plan := text
	for _, call := range calls {
		if call.Name == "write_plan" {
			var args struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(call.Input, &args); err != nil {
				continue
			}
			if args.Content != "" {
				plan = args.Content
			}
		}
	}

	if plan == "" {
		return "", agent.ErrRetryable{Reason: "planner returned empty plan"}
	}

	if err := os.MkdirAll(p.planDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir planDir: %w", err)
	}

	planPath := filepath.Join(p.planDir, task.ID+".md")
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		return "", fmt.Errorf("write plan: %w", err)
	}

	task.PlanPath = planPath
	return planPath, nil
}

func buildPlannerPrompt(task *store.Task, issueBody string) string {
	return fmt.Sprintf(`You are a senior Go engineer. Analyze the following GitHub issue and produce a detailed implementation plan.

Issue URL: %s
Issue body:
%s

Your plan must:
1. Identify which files to create or modify
2. List concrete steps with exact file paths and function signatures
3. Include test steps using Go's testing package
4. Be written in Markdown

Use the write_plan tool to save your final plan.`, task.IssueURL, issueBody)
}

func plannerTools() []agent.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "The full plan in Markdown"}
		},
		"required": ["content"]
	}`)
	return []agent.Tool{
		{Name: "write_plan", Description: "Save the implementation plan", InputSchema: schema},
	}
}
