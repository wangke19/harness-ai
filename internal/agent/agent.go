package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Agent is the LLM backend interface. The Go harness calls CompleteWithTools
// in a loop until no more tool calls are returned.
type Agent interface {
	Complete(ctx context.Context, prompt string) (string, error)
	CompleteWithTools(ctx context.Context, prompt string, tools []Tool) (string, []ToolCall, error)
}

// Tool defines a function the agent can invoke.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema object
}

// ToolCall is a single tool invocation requested by the agent.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ErrRetryable means the caller should retry (up to its retry limit).
type ErrRetryable struct{ Reason string }

func (e ErrRetryable) Error() string { return fmt.Sprintf("retryable: %s", e.Reason) }

// ErrBlocker means the task should move to Blocked state and notify the human.
type ErrBlocker struct{ Reason string }

func (e ErrBlocker) Error() string { return fmt.Sprintf("blocker: %s", e.Reason) }

// ErrFatal means the task should move to Failed state.
type ErrFatal struct{ Reason string }

func (e ErrFatal) Error() string { return fmt.Sprintf("fatal: %s", e.Reason) }
