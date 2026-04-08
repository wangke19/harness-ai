package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/wangke19/harness-ai/internal/agent"
	"github.com/wangke19/harness-ai/internal/store"
)

// Executor runs the executor agent loop: plan → code → PR.
type Executor struct {
	agent      agent.Agent
	repoPath   string
	maxRetries int
	cmdTimeout time.Duration
}

func New(a agent.Agent, repoPath string, maxRetries int) *Executor {
	return &Executor{
		agent:      a,
		repoPath:   repoPath,
		maxRetries: maxRetries,
		cmdTimeout: 10 * time.Minute,
	}
}

// Execute runs the executor for the given task.
func (e *Executor) Execute(ctx context.Context, task *store.Task) error {
	if task.RetryCount >= e.maxRetries {
		return agent.ErrBlocker{Reason: fmt.Sprintf("executor exceeded max retries (%d)", e.maxRetries)}
	}

	plan, err := os.ReadFile(task.PlanPath)
	if err != nil {
		return agent.ErrBlocker{Reason: fmt.Sprintf("cannot read plan %q: %v", task.PlanPath, err)}
	}

	branch := fmt.Sprintf("harness/%s", task.ID)
	worktreePath, err := CreateWorktree(ctx, e.repoPath, branch)
	if err != nil {
		return agent.ErrRetryable{Reason: fmt.Sprintf("create worktree: %v", err)}
	}
	defer DeleteWorktree(ctx, e.repoPath, worktreePath) //nolint:errcheck

	tools := e.executorTools()
	prompt := buildExecutorPrompt(task, string(plan), worktreePath)

	_, calls, err := e.agent.CompleteWithTools(ctx, prompt, tools)
	if err != nil {
		return agent.ErrRetryable{Reason: fmt.Sprintf("executor LLM error: %v", err)}
	}

	var prNumber int
	for _, call := range calls {
		result, callErr := e.executeToolCall(ctx, worktreePath, call)
		if callErr != nil {
			return agent.ErrRetryable{Reason: fmt.Sprintf("tool %s error: %v", call.Name, callErr)}
		}
		if call.Name == "create_pr" {
			_, _ = fmt.Sscanf(result, "%d", &prNumber)
		}
	}

	if prNumber == 0 {
		return agent.ErrRetryable{Reason: "executor did not create a PR"}
	}
	task.PRNumber = prNumber
	return nil
}

func (e *Executor) executeToolCall(ctx context.Context, workdir string, call agent.ToolCall) (string, error) {
	switch call.Name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return "", err
		}
		return "", WriteFile(workdir+"/"+args.Path, args.Content)

	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return "", err
		}
		return ReadFile(workdir + "/" + args.Path)

	case "run_command":
		var args struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return "", err
		}
		stdout, stderr, exit, err := RunCommand(ctx, workdir, args.Cmd, e.cmdTimeout)
		if err != nil {
			return "", err
		}
		if exit != 0 {
			return "", fmt.Errorf("command exited %d: %s", exit, stderr)
		}
		return stdout, nil

	case "create_pr":
		// Placeholder: actual GitHub API call wired in main orchestrator.
		return "0", nil

	default:
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func buildExecutorPrompt(task *store.Task, plan, worktreePath string) string {
	return fmt.Sprintf(`You are a senior Go engineer. Implement the following plan in the git worktree at %s.

Task ID: %s
Issue URL: %s

Implementation plan:
%s

Instructions:
- Use write_file to create or modify files (paths relative to worktree root)
- Use read_file to read existing files before editing
- Use run_command to run tests: "go test ./..."
- Use run_command to run lint: "golangci-lint run"
- Use create_pr to open a pull request when all tests pass

All tests must pass before calling create_pr.`, worktreePath, task.ID, task.IssueURL, plan)
}

func (e *Executor) executorTools() []agent.Tool {
	return []agent.Tool{
		{
			Name:        "write_file",
			Description: "Write content to a file in the worktree (path relative to worktree root)",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		},
		{
			Name:        "read_file",
			Description: "Read a file from the worktree",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		},
		{
			Name:        "run_command",
			Description: "Run an allowed shell command (go, make, golangci-lint, git) in the worktree",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`),
		},
		{
			Name:        "create_pr",
			Description: "Create a GitHub pull request and return its number",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}},"required":["title","body"]}`),
		},
	}
}
