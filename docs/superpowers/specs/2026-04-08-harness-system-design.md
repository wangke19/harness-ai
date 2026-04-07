# Harness Agentic System — Design Spec

**Date:** 2026-04-08  
**Status:** Approved  

---

## Overview

A Go-based harness agentic system that monitors GitHub/Jira issues and autonomously
implements them: planning → coding → reviewing → merging. Human steer, agents execute.

**References:**
- [OpenAI Harness Engineering](https://openai.com/index/harness-engineering/)
- [Anthropic: Designing Long-Running Apps](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- [Anthropic: Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)

---

## Constraints & Decisions

| Dimension | Decision | Rationale |
|---|---|---|
| Language | Go | Fits team profile; predictable conventions for build/test/lint |
| Project scope | Go projects initially | Known conventions; generalizable later |
| Trigger | GitHub/Jira issues | Automated pickup; humans mark issues ready via label/status |
| Final output | Merged PR | Full automation; green CI → auto-merge |
| Autonomy | Tiered | Small decisions: log and continue; hard blockers: pause + notify |
| LLM backend | Claude (single) | One SDK, one auth path; model varies per agent role |

---

## Architecture

Single Go binary (`harness`) using an event-driven worker pool with in-process channels.
No external message broker required.

```
Issue Watcher (poll GitHub/Jira)
    ↓ IssueEvent
Event Bus (in-process channel)
    ↓
Worker Pool (goroutines)
    ├── Planner Agent   (claude-sonnet-4-6)
    ├── Executor Agent  (claude-sonnet-4-5)
    └── Reviewer Agent  (claude-opus-4-6)
         ↓
    Merger + Notifier
         ↓
    SQLite (task state)
```

### Package Layout

```
harness/
├── cmd/harness/          # main entry point, config loading
├── internal/
│   ├── watcher/          # polls GitHub/Jira for new issues
│   ├── pipeline/         # event bus + worker pool + task state machine
│   ├── agent/            # LLM agent interface + Claude implementation
│   ├── planner/          # Planner Agent: issue → exec plan
│   ├── executor/         # Executor Agent: git worktree + code gen + PR
│   ├── reviewer/         # Reviewer Agent: tests + review + approve/reject
│   ├── merger/           # merges PR when all checks pass
│   ├── notifier/         # posts comments to GitHub/Jira on blockers
│   └── store/            # SQLite-backed task state persistence
└── config/               # YAML config (repos, models, thresholds)
```

---

## Agent Interface

```go
// internal/agent/agent.go
type Agent interface {
    Complete(ctx context.Context, prompt string) (string, error)
    CompleteWithTools(ctx context.Context, prompt string, tools []Tool) (string, []ToolCall, error)
}

type Tool struct {
    Name        string
    Description string
    InputSchema json.RawMessage
}

type ToolCall struct {
    Name  string
    Input json.RawMessage
}
```

### LLM Backend

One concrete implementation: `internal/agent/claude.go` wrapping `github.com/anthropics/anthropic-sdk-go`.

Model assignments per agent role:

```yaml
agents:
  planner:
    backend: claude
    model: claude-sonnet-4-6    # good reasoning, fast planning
  executor:
    backend: claude
    model: claude-sonnet-4-5    # cost-effective for code generation volume
  reviewer:
    backend: claude
    model: claude-opus-4-6      # highest stakes — catches bugs before merge
```

Optional backends (additive, not required for v1): `claudecode.go`, `opencode.go`,
`picoclaw.go`, `gemini.go` — each implements the same `Agent` interface.

---

## Task State Machine

```
Pending → Planning → Executing → Reviewing → Merging → Done
                         ↑            |
                         └────────────┘  (review rejected → re-execute)

Any state → Blocked (hard blocker: missing creds, too many retries, CI red)
Blocked → resumes from the state that triggered it (human resolves)
```

### SQLite Schema

```go
type TaskStatus string

const (
    StatusPending   TaskStatus = "pending"
    StatusPlanning  TaskStatus = "planning"
    StatusExecuting TaskStatus = "executing"
    StatusReviewing TaskStatus = "reviewing"
    StatusMerging   TaskStatus = "merging"
    StatusBlocked   TaskStatus = "blocked"
    StatusDone      TaskStatus = "done"
    StatusFailed    TaskStatus = "failed"
)

type Task struct {
    ID          string
    IssueURL    string
    Status      TaskStatus
    BlockReason string
    PlanPath    string     // exec-plans/<issue-id>.md
    PRNumber    int
    RetryCount  int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### Tiered Autonomy at State Transitions

| Situation | Action |
|---|---|
| Planner unsure about scope | Decide conservatively, note in plan |
| Executor tests fail (≤3 retries) | Auto-retry, log each attempt |
| Executor tests fail (>3 retries) | → `Blocked`, comment on issue |
| Reviewer finds issues | → back to `Executing` with review report |
| Missing credentials/secrets | → `Blocked` immediately |
| PR checks red after merge attempt | → `Blocked`, comment on PR |

**Restart survival:** On startup, load all non-terminal tasks from SQLite and re-queue
to the worker pool. Tasks in `Executing` restart from the beginning of that state
(idempotent — new git worktree, same branch name).

---

## Tools per Agent

The Go harness executes all tool calls. Claude decides what to call and with what arguments.

### Planner Tools

```go
read_issue(url string) string
read_file(path string) string
search_code(query string) []string
write_plan(content string) string      // writes to exec-plans/<issue-id>.md
```

### Executor Tools

```go
create_worktree(branch string) string
write_file(path, content string)
read_file(path string) string
run_command(cmd string) (stdout, stderr string, exit int)
create_pr(title, body, branch string) int
delete_worktree(path string)
```

### Reviewer Tools

```go
read_pr_diff(pr int) string
run_command(cmd string) (stdout, stderr string, exit int)
post_review_comment(pr int, body string)
approve_pr(pr int)
request_changes(pr int, body string)
```

### `run_command` Safety Constraints

- **Allowlist:** `go build`, `go test`, `make`, `golangci-lint`, `git` only
- **Timeout:** 10 minutes max per invocation
- **Working directory:** locked to the task's worktree path

---

## Watcher & Notifier

### Issue Watcher

Polls on a configurable interval (default 60s). Deduplicates via SQLite `seen` table.

```go
type IssueSource interface {
    FetchNew(ctx context.Context, since time.Time) ([]Issue, error)
}
```

**Trigger criteria (config):**

```yaml
watch:
  github:
    repo: wangke19/my-go-project
    label: "harness-ready"       # humans mark issues ready
  jira:
    project: MYPROJ
    status: "Ready for Dev"
```

### Notifier

Posts comments to GitHub issue/PR or Jira ticket — no separate dashboard needed.

```go
type Notifier interface {
    NotifyBlocked(ctx context.Context, task *Task, reason string) error
    NotifyDone(ctx context.Context, task *Task, prURL string) error
    NotifyFailed(ctx context.Context, task *Task, err error) error
}
```

---

## Error Handling

Three error tiers map to the tiered autonomy decisions:

```go
type ErrRetryable struct{ Reason string }  // auto-retry (up to RetryCount limit)
type ErrBlocker struct  { Reason string }  // → Blocked state, notify human
type ErrFatal struct    { Reason string }  // → Failed state, notify human
```

| Error | Tier |
|---|---|
| `go test` timeout / flake | `ErrRetryable` (≤3x) |
| LLM returns malformed plan | `ErrRetryable` (1x), then `ErrBlocker` |
| Missing `GITHUB_TOKEN` | `ErrBlocker` immediately |
| SQLite corruption | `ErrFatal` |

All errors logged as structured JSON with `task_id`, `state`, `agent`, `retry_count`.

---

## Testing Strategy

| Layer | Approach |
|---|---|
| `store/` | Unit tests with in-memory SQLite (`?mode=memory`) |
| `agent/claude.go` | Unit tests with recorded HTTP fixtures (no live API calls) |
| `planner/`, `executor/`, `reviewer/` | Integration tests with real Git repo + mock GitHub API |
| `watcher/` | Unit tests with mock `IssueSource` returning fixture data |
| End-to-end | One E2E test against a real test repo with a real GitHub token |

---

## Configuration Reference

```yaml
# config/config.yaml
server:
  poll_interval: 60s
  worker_count: 3
  db_path: ./harness.db

agents:
  planner:
    backend: claude
    model: claude-sonnet-4-6
  executor:
    backend: claude
    model: claude-sonnet-4-5
  reviewer:
    backend: claude
    model: claude-opus-4-6

watch:
  github:
    repo: owner/repo
    label: harness-ready
  jira:
    project: PROJ
    status: "Ready for Dev"

executor:
  max_retries: 3
  command_timeout: 10m
  allowed_commands:
    - go
    - make
    - golangci-lint
    - git
```

---

## Out of Scope (v1)

- Multi-language projects (Go only for now)
- Parallel execution of multiple tasks for the same repo
- Visual progress dashboard
- Self-hosted LLM backends
- Automatic learning/feedback loop
