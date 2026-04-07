package reviewer

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
	"github.com/wangke19/harness-system/internal/agent"
	"github.com/wangke19/harness-system/internal/store"
)

type Decision string

const (
	DecisionApprove        Decision = "approve"
	DecisionRequestChanges Decision = "request_changes"
)

// Reviewer runs the reviewer agent: PR diff → approve or request_changes.
type Reviewer struct {
	agent  agent.Agent
	github *github.Client
}

func New(a agent.Agent, ghClient *github.Client) *Reviewer {
	return &Reviewer{agent: a, github: ghClient}
}

// Review analyzes a PR diff and returns the review decision.
func (r *Reviewer) Review(ctx context.Context, task *store.Task, diff string) (Decision, error) {
	prompt := buildReviewerPrompt(task, diff)
	text, err := r.agent.Complete(ctx, prompt)
	if err != nil {
		return "", agent.ErrRetryable{Reason: fmt.Sprintf("reviewer LLM error: %v", err)}
	}

	upper := strings.ToUpper(strings.TrimSpace(text))
	if strings.HasPrefix(upper, "APPROVE") {
		return DecisionApprove, nil
	}
	if strings.HasPrefix(upper, "REQUEST_CHANGES") {
		return DecisionRequestChanges, nil
	}
	// Default to request changes on ambiguous output.
	return DecisionRequestChanges, nil
}

func buildReviewerPrompt(task *store.Task, diff string) string {
	return fmt.Sprintf(`You are a senior Go engineer reviewing a pull request.

Task ID: %s
Issue URL: %s
PR Number: %d

PR diff:
%s

Review criteria:
1. All tests pass (assume CI ran — focus on logic and code quality)
2. No obvious bugs or security issues
3. Code follows Go idioms (error handling, context usage, naming)
4. Changes are minimal and focused on the issue

Respond with exactly one of:
- APPROVE
- REQUEST_CHANGES: <specific reason>`, task.ID, task.IssueURL, task.PRNumber, diff)
}
