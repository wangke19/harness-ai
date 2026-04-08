# Harness AI System — Architecture

## Overview

A Go-based agentic system that monitors GitHub/Jira issues and autonomously implements them end-to-end: planning → coding → reviewing → merging.

**Core principle:** Human steer, Agents execute.

**References:**
- [OpenAI Harness Engineering](https://openai.com/index/harness-engineering/)
- [Anthropic: Designing Long-Running Apps](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- [Anthropic: Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)

---

## System Flow

```
GitHub / Jira Issues
        ↓  (poll every 60s, label: "harness-ready")
    Watcher
        ↓  IssueEvent
    Event Bus (in-process channel)
        ↓
    Worker Pool (goroutines)
        ↓
    Planner Agent  (claude-sonnet-4-6)
        ↓  exec-plans/<issue-id>.md
    Executor Agent (claude-sonnet-4-5)
        ↓  git worktree → code → PR
    Reviewer Agent (claude-opus-4-6)
        ↓  approve / request_changes
    Merger
        ↓  squash merge when CI green
    Notifier → GitHub/Jira comment
```

---

## Package Layout

```
github.com/wangke19/harness-ai
├── cmd/harness/          # Entry point: config loading, component wiring, signal handling
├── config/               # YAML config parsing (server, agents, watch, executor)
└── internal/
    ├── agent/            # Agent interface + Claude SDK implementation
    ├── watcher/          # Issue polling (GitHub, Jira), seen-issue dedup
    ├── pipeline/         # Worker pool, task state machine, Transition helper
    ├── store/            # SQLite: task CRUD + seen_issues table
    ├── planner/          # Planner agent loop: issue → exec-plan Markdown
    ├── executor/         # Executor agent loop: plan → git worktree → code → PR
    │   └── tools.go      # RunCommand (allowlisted), CreateWorktree, WriteFile, ReadFile
    ├── reviewer/         # Reviewer agent loop: PR diff → APPROVE / REQUEST_CHANGES
    ├── merger/           # Poll PR checks, squash-merge, get diff
    └── notifier/         # Post GitHub comments on blocked/done/failed
```

---

## Task State Machine

```
Pending → Planning → Executing → Reviewing → Merging → Done
                         ↑            |
                         └────────────┘  (review rejected → re-execute)

Any state → Blocked  (hard blocker: missing creds, max retries exceeded, CI red)
Blocked   → resumes from the state that triggered it after human resolves
Any state → Failed   (unrecoverable error)
```

**Tiered autonomy:**

| Situation | Action |
|---|---|
| Planner unsure about scope | Decide conservatively, note in plan |
| Executor tests fail ≤3 retries | Auto-retry, log each attempt |
| Executor tests fail >3 retries | → Blocked, comment on issue |
| Reviewer finds issues | → back to Executing with review report |
| Missing credentials/secrets | → Blocked immediately |
| PR checks red after merge attempt | → Blocked, comment on PR |

**Restart survival:** On startup, non-terminal tasks are reloaded from SQLite and re-queued to the worker pool.

---

## Agent Interface

All LLM backends implement a single interface. The Go harness executes every tool call — the LLM only decides what to call and with what arguments.

```go
type Agent interface {
    Complete(ctx context.Context, prompt string) (string, error)
    CompleteWithTools(ctx context.Context, prompt string, tools []Tool) (string, []ToolCall, error)
}
```

**Model assignments:**

| Agent | Model | Reason |
|---|---|---|
| Planner | `claude-sonnet-4-6` | Good reasoning, fast planning |
| Executor | `claude-sonnet-4-5` | Cost-effective for high-volume code generation |
| Reviewer | `claude-opus-4-6` | Highest stakes — catches bugs before merge |

**Optional backends** (additive, not required for v1): OpenCode, Claude Code CLI, PicoClaw, Gemini — each implements the same `Agent` interface.

---

## Tools per Agent

### Planner
```
read_issue    — fetch issue body from GitHub/Jira
read_file     — read existing code for context
search_code   — grep codebase for relevant patterns
write_plan    — write exec-plans/<issue-id>.md
```

### Executor
```
create_worktree  — git worktree add -b harness/<task-id>
write_file       — create/overwrite file in worktree
read_file        — read file from worktree
run_command      — execute allowlisted command (go, make, golangci-lint, git)
create_pr        — open GitHub PR, return PR number
delete_worktree  — cleanup after PR created
```

### Reviewer
```
read_pr_diff         — fetch PR patch from GitHub
run_command          — run tests/lint
post_review_comment  — comment on PR
approve_pr           — GitHub API approve
request_changes      — send task back to Executing
```

**`run_command` safety:**
- Allowlist: `go`, `make`, `golangci-lint`, `git` only
- Timeout: 10 minutes per invocation
- Working directory: locked to the task's git worktree path

---

## Error Handling

```go
type ErrRetryable struct{ Reason string }  // auto-retry up to max_retries
type ErrBlocker  struct{ Reason string }   // → Blocked state + notify human
type ErrFatal    struct{ Reason string }   // → Failed state + notify human
```

| Situation | Error type |
|---|---|
| `go test` timeout / flake | `ErrRetryable` (≤3×) |
| LLM returns malformed plan | `ErrRetryable` (1×), then `ErrBlocker` |
| Missing `GITHUB_TOKEN` | `ErrBlocker` immediately |
| SQLite corruption | `ErrFatal` |

All errors logged as structured JSON with `task_id`, `state`, `agent`, `retry_count`.

---

## Configuration

```yaml
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
    label: harness-ready       # humans mark issues ready
  jira:
    project: PROJ
    status: "Ready for Dev"

executor:
  max_retries: 3
  command_timeout: 10m
  allowed_commands: [go, make, golangci-lint, git]
```

---

## Agent Communication

Agents communicate through artifacts, not direct calls:

```
Planner  → Executor : exec-plans/<issue-id>.md
Executor → Reviewer : GitHub PR (number stored in Task.PRNumber)
Reviewer → Executor : review decision (DecisionRequestChanges triggers re-execute)
Any      → Human    : GitHub/Jira comment via Notifier
```

---

## Security

- **Command allowlist:** `run_command` rejects anything outside `{go, make, golangci-lint, git}`
- **Worktree isolation:** each task works in `/tmp/harness-<branch>`, never the main checkout
- **Credentials:** read from env vars (`ANTHROPIC_API_KEY`, `GITHUB_TOKEN`), never logged
- **Rollback:** every change is on a branch; human can close the PR

---

## Out of Scope (v1)

- Multi-language projects (Go only)
- Parallel execution of multiple tasks on the same repo
- Visual progress dashboard
- Self-hosted LLM backends
- Automatic learning / feedback loop
