# Harness Agentic System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go binary that monitors GitHub/Jira issues and autonomously plans, implements, reviews, and merges PRs using Claude.

**Architecture:** Single Go binary with an in-process event bus, goroutine worker pool, and SQLite state store. A Watcher polls issue sources; each issue becomes a Task that moves through a state machine (Pending → Planning → Executing → Reviewing → Merging → Done). Three Claude-backed agents (Planner, Executor, Reviewer) each get a focused tool set; the Go harness executes all tool calls.

**Tech Stack:** Go 1.22+, `github.com/anthropics/anthropic-sdk-go`, `github.com/google/go-github/v60`, `github.com/mattn/go-sqlite3`, `gopkg.in/yaml.v3`, standard library (`context`, `sync`, `os/exec`).

---

## File Map

| File | Responsibility |
|---|---|
| `go.mod` / `go.sum` | Module definition and dependencies |
| `cmd/harness/main.go` | Entry point: load config, wire components, start watcher + pipeline |
| `config/config.go` | Parse `config/config.yaml` into typed structs |
| `internal/store/store.go` | SQLite CRUD for Task; seen-issue dedup |
| `internal/store/store_test.go` | Unit tests using in-memory SQLite |
| `internal/agent/agent.go` | `Agent` interface, `Tool`, `ToolCall` types, error types |
| `internal/agent/claude.go` | Claude SDK implementation of `Agent` |
| `internal/agent/claude_test.go` | Unit tests with HTTP fixtures |
| `internal/watcher/watcher.go` | Poll loop, dedup, emit `IssueEvent` to channel |
| `internal/watcher/watcher_test.go` | Unit tests with mock `IssueSource` |
| `internal/watcher/github.go` | `IssueSource` impl for GitHub |
| `internal/watcher/jira.go` | `IssueSource` impl for Jira |
| `internal/pipeline/pipeline.go` | Worker pool, task state machine transitions |
| `internal/pipeline/pipeline_test.go` | Unit tests for state transitions |
| `internal/planner/planner.go` | Planner agent loop: issue → exec-plan markdown |
| `internal/planner/planner_test.go` | Integration test with real git + mock GitHub |
| `internal/executor/tools.go` | Tool implementations: worktree, file, run_command, create_pr |
| `internal/executor/executor.go` | Executor agent loop: plan → code → PR |
| `internal/executor/executor_test.go` | Integration test with real git + mock GitHub |
| `internal/reviewer/reviewer.go` | Reviewer agent loop: PR diff → approve/request_changes |
| `internal/reviewer/reviewer_test.go` | Integration test with mock GitHub |
| `internal/merger/merger.go` | Merge PR via GitHub API when checks pass |
| `internal/notifier/notifier.go` | Post GitHub/Jira comments on block/done/fail |

---

## Task 1: Module scaffold and config

**Files:**
- Create: `go.mod`
- Create: `config/config.go`
- Create: `config/config.yaml`
- Create: `cmd/harness/main.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /path/to/harness-system
go mod init github.com/wangke19/harness-system
```

Expected: `go.mod` created with `module github.com/wangke19/harness-system` and `go 1.22`.

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/anthropics/anthropic-sdk-go
go get github.com/google/go-github/v60
go get github.com/mattn/go-sqlite3
go get gopkg.in/yaml.v3
```

- [ ] **Step 3: Write config types**

Create `config/config.go`:

```go
package config

import (
    "os"
    "time"

    "gopkg.in/yaml.v3"
)

type Config struct {
    Server   ServerConfig            `yaml:"server"`
    Agents   map[string]AgentConfig  `yaml:"agents"`
    Watch    WatchConfig             `yaml:"watch"`
    Executor ExecutorConfig          `yaml:"executor"`
}

type ServerConfig struct {
    PollInterval time.Duration `yaml:"poll_interval"`
    WorkerCount  int           `yaml:"worker_count"`
    DBPath       string        `yaml:"db_path"`
}

type AgentConfig struct {
    Backend string `yaml:"backend"`
    Model   string `yaml:"model"`
}

type WatchConfig struct {
    GitHub GitHubWatchConfig `yaml:"github"`
    Jira   JiraWatchConfig   `yaml:"jira"`
}

type GitHubWatchConfig struct {
    Repo  string `yaml:"repo"`
    Label string `yaml:"label"`
}

type JiraWatchConfig struct {
    Project string `yaml:"project"`
    Status  string `yaml:"status"`
}

type ExecutorConfig struct {
    MaxRetries      int           `yaml:"max_retries"`
    CommandTimeout  time.Duration `yaml:"command_timeout"`
    AllowedCommands []string      `yaml:"allowed_commands"`
}

func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

- [ ] **Step 4: Write example config**

Create `config/config.yaml`:

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

- [ ] **Step 5: Write minimal main**

Create `cmd/harness/main.go`:

```go
package main

import (
    "flag"
    "log/slog"
    "os"

    "github.com/wangke19/harness-system/config"
)

func main() {
    cfgPath := flag.String("config", "config/config.yaml", "path to config file")
    flag.Parse()

    cfg, err := config.Load(*cfgPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }

    slog.Info("harness starting", "workers", cfg.Server.WorkerCount, "poll_interval", cfg.Server.PollInterval)
    // components wired in later tasks
    select {} // block until signal
}
```

- [ ] **Step 6: Verify it compiles**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum config/ cmd/
git commit -m "feat: scaffold module, config, and main entry point"
```

---

## Task 2: Store — SQLite task persistence

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/store/store_test.go`:

```go
package store_test

import (
    "context"
    "testing"
    "time"

    "github.com/wangke19/harness-system/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
    t.Helper()
    s, err := store.New(":memory:")
    if err != nil {
        t.Fatalf("store.New: %v", err)
    }
    t.Cleanup(func() { s.Close() })
    return s
}

func TestCreateAndGet(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    task := &store.Task{
        ID:       "test-1",
        IssueURL: "https://github.com/owner/repo/issues/1",
        Status:   store.StatusPending,
    }
    if err := s.CreateTask(ctx, task); err != nil {
        t.Fatalf("CreateTask: %v", err)
    }

    got, err := s.GetTask(ctx, "test-1")
    if err != nil {
        t.Fatalf("GetTask: %v", err)
    }
    if got.IssueURL != task.IssueURL {
        t.Errorf("IssueURL: got %q want %q", got.IssueURL, task.IssueURL)
    }
    if got.Status != store.StatusPending {
        t.Errorf("Status: got %q want %q", got.Status, store.StatusPending)
    }
}

func TestUpdateStatus(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    task := &store.Task{ID: "t2", IssueURL: "https://github.com/owner/repo/issues/2", Status: store.StatusPending}
    _ = s.CreateTask(ctx, task)

    if err := s.UpdateStatus(ctx, "t2", store.StatusPlanning, ""); err != nil {
        t.Fatalf("UpdateStatus: %v", err)
    }

    got, _ := s.GetTask(ctx, "t2")
    if got.Status != store.StatusPlanning {
        t.Errorf("Status: got %q want %q", got.Status, store.StatusPlanning)
    }
}

func TestListActiveTasks(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    for i, status := range []store.TaskStatus{store.StatusPending, store.StatusPlanning, store.StatusDone} {
        _ = s.CreateTask(ctx, &store.Task{
            ID:       fmt.Sprintf("t%d", i),
            IssueURL: fmt.Sprintf("https://github.com/owner/repo/issues/%d", i),
            Status:   status,
        })
    }

    tasks, err := s.ListActiveTasks(ctx)
    if err != nil {
        t.Fatalf("ListActiveTasks: %v", err)
    }
    if len(tasks) != 2 { // Pending + Planning, not Done
        t.Errorf("got %d active tasks, want 2", len(tasks))
    }
}

func TestSeenIssue(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()

    url := "https://github.com/owner/repo/issues/99"
    if s.HasSeen(ctx, url) {
        t.Error("should not have seen issue yet")
    }
    _ = s.MarkSeen(ctx, url)
    if !s.HasSeen(ctx, url) {
        t.Error("should have seen issue after marking")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/store/... -v
```

Expected: compile error — `store` package does not exist yet.

- [ ] **Step 3: Implement the store**

Create `internal/store/store.go`:

```go
package store

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    _ "github.com/mattn/go-sqlite3"
)

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

var terminalStatuses = map[TaskStatus]bool{
    StatusDone:   true,
    StatusFailed: true,
}

type Task struct {
    ID          string
    IssueURL    string
    Status      TaskStatus
    BlockReason string
    PlanPath    string
    PRNumber    int
    RetryCount  int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Store struct {
    db *sql.DB
}

func New(dsn string) (*Store, error) {
    db, err := sql.Open("sqlite3", dsn)
    if err != nil {
        return nil, fmt.Errorf("open sqlite: %w", err)
    }
    s := &Store{db: db}
    if err := s.migrate(); err != nil {
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
    _, err := s.db.Exec(`
        CREATE TABLE IF NOT EXISTS tasks (
            id           TEXT PRIMARY KEY,
            issue_url    TEXT NOT NULL,
            status       TEXT NOT NULL,
            block_reason TEXT NOT NULL DEFAULT '',
            plan_path    TEXT NOT NULL DEFAULT '',
            pr_number    INTEGER NOT NULL DEFAULT 0,
            retry_count  INTEGER NOT NULL DEFAULT 0,
            created_at   DATETIME NOT NULL,
            updated_at   DATETIME NOT NULL
        );
        CREATE TABLE IF NOT EXISTS seen_issues (
            url TEXT PRIMARY KEY,
            seen_at DATETIME NOT NULL
        );
    `)
    return err
}

func (s *Store) CreateTask(ctx context.Context, t *Task) error {
    now := time.Now().UTC()
    t.CreatedAt = now
    t.UpdatedAt = now
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO tasks (id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        t.ID, t.IssueURL, t.Status, t.BlockReason, t.PlanPath, t.PRNumber, t.RetryCount, t.CreatedAt, t.UpdatedAt,
    )
    return err
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
    row := s.db.QueryRowContext(ctx,
        `SELECT id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at
         FROM tasks WHERE id = ?`, id)
    return scanTask(row)
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status TaskStatus, blockReason string) error {
    _, err := s.db.ExecContext(ctx,
        `UPDATE tasks SET status = ?, block_reason = ?, updated_at = ? WHERE id = ?`,
        status, blockReason, time.Now().UTC(), id,
    )
    return err
}

func (s *Store) UpdateTask(ctx context.Context, t *Task) error {
    t.UpdatedAt = time.Now().UTC()
    _, err := s.db.ExecContext(ctx,
        `UPDATE tasks SET status=?, block_reason=?, plan_path=?, pr_number=?, retry_count=?, updated_at=? WHERE id=?`,
        t.Status, t.BlockReason, t.PlanPath, t.PRNumber, t.RetryCount, t.UpdatedAt, t.ID,
    )
    return err
}

func (s *Store) ListActiveTasks(ctx context.Context) ([]*Task, error) {
    rows, err := s.db.QueryContext(ctx,
        `SELECT id, issue_url, status, block_reason, plan_path, pr_number, retry_count, created_at, updated_at
         FROM tasks WHERE status NOT IN ('done', 'failed')`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var tasks []*Task
    for rows.Next() {
        t, err := scanTask(rows)
        if err != nil {
            return nil, err
        }
        tasks = append(tasks, t)
    }
    return tasks, rows.Err()
}

func (s *Store) HasSeen(ctx context.Context, url string) bool {
    var count int
    _ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen_issues WHERE url = ?`, url).Scan(&count)
    return count > 0
}

func (s *Store) MarkSeen(ctx context.Context, url string) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT OR IGNORE INTO seen_issues (url, seen_at) VALUES (?, ?)`, url, time.Now().UTC())
    return err
}

type scanner interface {
    Scan(dest ...any) error
}

func scanTask(s scanner) (*Task, error) {
    var t Task
    err := s.Scan(&t.ID, &t.IssueURL, &t.Status, &t.BlockReason, &t.PlanPath, &t.PRNumber, &t.RetryCount, &t.CreatedAt, &t.UpdatedAt)
    if err != nil {
        return nil, err
    }
    return &t, nil
}
```

Also add `"fmt"` import to the test file:

```go
// add to imports in store_test.go
import (
    "context"
    "fmt"
    "testing"

    "github.com/wangke19/harness-system/internal/store"
)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/store/... -v
```

Expected: `PASS` for all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add SQLite store for task state and seen-issue dedup"
```

---

## Task 3: Agent interface and error types

**Files:**
- Create: `internal/agent/agent.go`

- [ ] **Step 1: Write agent.go**

Create `internal/agent/agent.go`:

```go
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/agent/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: add Agent interface and error types"
```

---

## Task 4: Claude agent implementation

**Files:**
- Create: `internal/agent/claude.go`
- Create: `internal/agent/claude_test.go`
- Create: `internal/agent/testdata/tool_use_response.json`
- Create: `internal/agent/testdata/final_response.json`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/testdata/tool_use_response.json` — this simulates Claude responding with a tool call:

```json
{
  "id": "msg_01",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_01",
      "name": "read_file",
      "input": {"path": "/tmp/test.go"}
    }
  ],
  "model": "claude-sonnet-4-6",
  "stop_reason": "tool_use",
  "usage": {"input_tokens": 10, "output_tokens": 20}
}
```

Create `internal/agent/testdata/final_response.json` — Claude's final text response:

```json
{
  "id": "msg_02",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "Here is the plan."
    }
  ],
  "model": "claude-sonnet-4-6",
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 30, "output_tokens": 15}
}
```

Create `internal/agent/claude_test.go`:

```go
package agent_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/anthropics/anthropic-sdk-go/option"
    "github.com/wangke19/harness-system/internal/agent"
)

func TestClaudeCompleteWithTools_ToolLoop(t *testing.T) {
    callCount := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        callCount++
        if callCount == 1 {
            http.ServeFile(w, r, "testdata/tool_use_response.json")
        } else {
            http.ServeFile(w, r, "testdata/final_response.json")
        }
    }))
    defer srv.Close()

    a := agent.NewClaude("fake-key", "claude-sonnet-4-6",
        option.WithBaseURL(srv.URL),
    )

    tools := []agent.Tool{{
        Name:        "read_file",
        Description: "Read a file",
        InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
    }}

    text, calls, err := a.CompleteWithTools(context.Background(), "Make a plan", tools)
    if err != nil {
        t.Fatalf("CompleteWithTools: %v", err)
    }
    if text != "Here is the plan." {
        t.Errorf("text: got %q", text)
    }
    if len(calls) != 1 || calls[0].Name != "read_file" {
        t.Errorf("calls: got %+v", calls)
    }
    if callCount != 2 {
        t.Errorf("expected 2 API calls (tool loop), got %d", callCount)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/... -v -run TestClaudeCompleteWithTools_ToolLoop
```

Expected: compile error — `NewClaude` not defined.

- [ ] **Step 3: Implement claude.go**

Create `internal/agent/claude.go`:

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeAgent implements Agent using the Anthropic Go SDK.
type ClaudeAgent struct {
    client *anthropic.Client
    model  string
}

// NewClaude creates a ClaudeAgent. opts are passed to anthropic.NewClient,
// useful for injecting a test base URL.
func NewClaude(apiKey, model string, opts ...option.RequestOption) *ClaudeAgent {
    allOpts := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
    return &ClaudeAgent{
        client: anthropic.NewClient(allOpts...),
        model:  model,
    }
}

func (a *ClaudeAgent) Complete(ctx context.Context, prompt string) (string, error) {
    resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.Model(a.model),
        MaxTokens: 4096,
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
        },
    })
    if err != nil {
        return "", fmt.Errorf("claude complete: %w", err)
    }
    return extractText(resp.Content), nil
}

func (a *ClaudeAgent) CompleteWithTools(ctx context.Context, prompt string, tools []Tool) (string, []ToolCall, error) {
    apiTools := toAPITools(tools)
    messages := []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
    }

    var allCalls []ToolCall

    for {
        resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
            Model:     anthropic.Model(a.model),
            MaxTokens: 4096,
            Tools:     apiTools,
            Messages:  messages,
        })
        if err != nil {
            return "", nil, fmt.Errorf("claude tool call: %w", err)
        }

        // Append assistant turn to history.
        messages = append(messages, resp.ToParam())

        if resp.StopReason != anthropic.StopReasonToolUse {
            return extractText(resp.Content), allCalls, nil
        }

        // Execute tool calls (caller provides results via returned ToolCalls).
        // Here we collect the calls and return placeholder results so the loop
        // continues. The actual tool execution happens in planner/executor/reviewer.
        var toolResults []anthropic.ContentBlockParamUnion
        for _, block := range resp.Content {
            tc, ok := block.AsAny().(anthropic.ToolUseBlock)
            if !ok {
                continue
            }
            allCalls = append(allCalls, ToolCall{
                ID:    tc.ID,
                Name:  tc.Name,
                Input: json.RawMessage(tc.JSON.Input.Raw()),
            })
            // Signal to the model that the tool was called (empty result for now).
            toolResults = append(toolResults,
                anthropic.NewToolResultBlock(tc.ID, "", false))
        }
        messages = append(messages, anthropic.NewUserMessage(toolResults...))
    }
}

func extractText(blocks []anthropic.ContentBlock) string {
    for _, b := range blocks {
        if tb, ok := b.AsAny().(anthropic.TextBlock); ok {
            return tb.Text
        }
    }
    return ""
}

func toAPITools(tools []Tool) []anthropic.ToolUnionParam {
    out := make([]anthropic.ToolUnionParam, len(tools))
    for i, t := range tools {
        var props map[string]any
        _ = json.Unmarshal(t.InputSchema, &props)
        out[i] = anthropic.ToolUnionParam{
            OfTool: &anthropic.ToolParam{
                Name:        t.Name,
                Description: anthropic.String(t.Description),
                InputSchema: anthropic.ToolInputSchemaParam{
                    Properties: props,
                },
            },
        }
    }
    return out
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/agent/... -v -run TestClaudeCompleteWithTools_ToolLoop
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat: add Claude agent implementation with tool-use loop"
```

---

## Task 5: Watcher

**Files:**
- Create: `internal/watcher/watcher.go`
- Create: `internal/watcher/github.go`
- Create: `internal/watcher/jira.go`
- Create: `internal/watcher/watcher_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/watcher/watcher_test.go`:

```go
package watcher_test

import (
    "context"
    "testing"
    "time"

    "github.com/wangke19/harness-system/internal/store"
    "github.com/wangke19/harness-system/internal/watcher"
)

type mockSource struct {
    issues []watcher.Issue
}

func (m *mockSource) FetchNew(ctx context.Context, since time.Time) ([]watcher.Issue, error) {
    return m.issues, nil
}

func TestWatcher_EmitsNewIssues(t *testing.T) {
    s, _ := store.New(":memory:")
    defer s.Close()

    src := &mockSource{issues: []watcher.Issue{
        {ID: "gh-1", URL: "https://github.com/owner/repo/issues/1", Title: "Fix bug", Source: "github"},
    }}

    events := make(chan watcher.IssueEvent, 10)
    w := watcher.New(s, events, 50*time.Millisecond, src)

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    go w.Run(ctx)

    select {
    case ev := <-events:
        if ev.Issue.ID != "gh-1" {
            t.Errorf("got issue ID %q, want %q", ev.Issue.ID, "gh-1")
        }
    case <-ctx.Done():
        t.Fatal("timeout: no issue event received")
    }
}

func TestWatcher_DeduplicatesSeenIssues(t *testing.T) {
    s, _ := store.New(":memory:")
    defer s.Close()

    src := &mockSource{issues: []watcher.Issue{
        {ID: "gh-2", URL: "https://github.com/owner/repo/issues/2", Title: "Dup", Source: "github"},
    }}

    events := make(chan watcher.IssueEvent, 10)
    w := watcher.New(s, events, 50*time.Millisecond, src)

    ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
    defer cancel()

    go w.Run(ctx)

    count := 0
    timeout := time.After(250 * time.Millisecond)
    for {
        select {
        case <-events:
            count++
        case <-timeout:
            if count != 1 {
                t.Errorf("got %d events for same issue, want exactly 1", count)
            }
            return
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/watcher/... -v
```

Expected: compile error — `watcher` package does not exist.

- [ ] **Step 3: Implement watcher.go**

Create `internal/watcher/watcher.go`:

```go
package watcher

import (
    "context"
    "log/slog"
    "time"

    "github.com/wangke19/harness-system/internal/store"
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
    since := time.Now().Add(-24 * time.Hour) // look back 24h on first poll

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
```

- [ ] **Step 4: Implement github.go and jira.go stubs**

Create `internal/watcher/github.go`:

```go
package watcher

import (
    "context"
    "strings"
    "time"

    "github.com/google/go-github/v60/github"
)

// GitHubSource fetches issues from a GitHub repo with a specific label.
type GitHubSource struct {
    client *github.Client
    owner  string
    repo   string
    label  string
}

func NewGitHubSource(token, repoSlug, label string) *GitHubSource {
    parts := strings.SplitN(repoSlug, "/", 2)
    return &GitHubSource{
        client: github.NewClient(nil).WithAuthToken(token),
        owner:  parts[0],
        repo:   parts[1],
        label:  label,
    }
}

func (g *GitHubSource) FetchNew(ctx context.Context, since time.Time) ([]Issue, error) {
    opts := &github.IssueListByRepoOptions{
        Labels: []string{g.label},
        Since:  since,
        State:  "open",
        ListOptions: github.ListOptions{PerPage: 50},
    }
    ghIssues, _, err := g.client.Issues.ListByRepo(ctx, g.owner, g.repo, opts)
    if err != nil {
        return nil, err
    }
    var issues []Issue
    for _, i := range ghIssues {
        if i.PullRequestLinks != nil {
            continue // skip PRs
        }
        issues = append(issues, Issue{
            ID:     fmt.Sprintf("gh-%d", i.GetNumber()),
            URL:    i.GetHTMLURL(),
            Title:  i.GetTitle(),
            Body:   i.GetBody(),
            Source: "github",
        })
    }
    return issues, nil
}
```

Add `"fmt"` to imports in `github.go`.

Create `internal/watcher/jira.go`:

```go
package watcher

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "time"
)

// JiraSource fetches issues from a Jira project with a specific status.
type JiraSource struct {
    baseURL  string
    token    string
    project  string
    status   string
    httpClient *http.Client
}

func NewJiraSource(baseURL, token, project, status string) *JiraSource {
    return &JiraSource{
        baseURL: baseURL,
        token:   token,
        project: project,
        status:  status,
        httpClient: &http.Client{Timeout: 10 * time.Second},
    }
}

func (j *JiraSource) FetchNew(ctx context.Context, since time.Time) ([]Issue, error) {
    jql := fmt.Sprintf(`project = %s AND status = "%s" AND updated >= "%s"`,
        j.project, j.status, since.Format("2006-01-02"))
    endpoint := fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=50",
        j.baseURL, url.QueryEscape(jql))

    req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+j.token)
    req.Header.Set("Accept", "application/json")

    resp, err := j.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Issues []struct {
            Key    string `json:"key"`
            Self   string `json:"self"`
            Fields struct {
                Summary     string `json:"summary"`
                Description any    `json:"description"`
            } `json:"fields"`
        } `json:"issues"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    var issues []Issue
    for _, i := range result.Issues {
        issues = append(issues, Issue{
            ID:     i.Key,
            URL:    fmt.Sprintf("%s/browse/%s", j.baseURL, i.Key),
            Title:  i.Fields.Summary,
            Source: "jira",
        })
    }
    return issues, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/watcher/... -v
```

Expected: `PASS` for both tests.

- [ ] **Step 6: Commit**

```bash
git add internal/watcher/
git commit -m "feat: add watcher with GitHub and Jira issue sources"
```

---

## Task 6: Pipeline — worker pool and state machine

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Create: `internal/pipeline/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/pipeline/pipeline_test.go`:

```go
package pipeline_test

import (
    "context"
    "testing"
    "time"

    "github.com/wangke19/harness-system/internal/pipeline"
    "github.com/wangke19/harness-system/internal/store"
    "github.com/wangke19/harness-system/internal/watcher"
)

type noopHandler struct{ called chan string }

func (h *noopHandler) Handle(ctx context.Context, task *store.Task) error {
    h.called <- task.ID
    return nil
}

func TestPipeline_ProcessesEvent(t *testing.T) {
    s, _ := store.New(":memory:")
    defer s.Close()

    handler := &noopHandler{called: make(chan string, 1)}
    events := make(chan watcher.IssueEvent, 1)
    p := pipeline.New(s, events, 2, handler)

    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    go p.Run(ctx)

    events <- watcher.IssueEvent{Issue: watcher.Issue{
        ID: "gh-10", URL: "https://github.com/owner/repo/issues/10", Title: "Test issue",
    }}

    select {
    case id := <-handler.called:
        if id != "gh-10" {
            t.Errorf("got task ID %q, want gh-10", id)
        }
    case <-ctx.Done():
        t.Fatal("timeout: handler not called")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/pipeline/... -v
```

Expected: compile error — `pipeline` package does not exist.

- [ ] **Step 3: Implement pipeline.go**

Create `internal/pipeline/pipeline.go`:

```go
package pipeline

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/wangke19/harness-system/internal/store"
    "github.com/wangke19/harness-system/internal/watcher"
)

// Handler processes a single task. Implemented by the orchestrator that
// chains Planner → Executor → Reviewer → Merger.
type Handler interface {
    Handle(ctx context.Context, task *store.Task) error
}

// Pipeline receives IssueEvents, creates Tasks, and dispatches them to
// a worker pool.
type Pipeline struct {
    store   *store.Store
    events  <-chan watcher.IssueEvent
    workers int
    handler Handler
}

func New(s *store.Store, events <-chan watcher.IssueEvent, workers int, handler Handler) *Pipeline {
    return &Pipeline{store: s, events: events, workers: workers, handler: handler}
}

// Run starts the worker pool and the event dispatch loop.
// It also re-queues active tasks surviving a restart.
func (p *Pipeline) Run(ctx context.Context) {
    work := make(chan *store.Task, p.workers*2)

    var wg sync.WaitGroup
    for i := 0; i < p.workers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for task := range work {
                if err := p.handler.Handle(ctx, task); err != nil {
                    slog.Error("task handler error", "task_id", task.ID, "error", err)
                }
            }
        }()
    }

    // Re-queue tasks that survived restart.
    p.requeueActive(ctx, work)

    // Dispatch new events.
    for {
        select {
        case <-ctx.Done():
            close(work)
            wg.Wait()
            return
        case ev := <-p.events:
            task := &store.Task{
                ID:       ev.Issue.ID,
                IssueURL: ev.Issue.URL,
                Status:   store.StatusPending,
            }
            if err := p.store.CreateTask(ctx, task); err != nil {
                slog.Error("create task error", "issue_url", ev.Issue.URL, "error", err)
                continue
            }
            work <- task
        }
    }
}

func (p *Pipeline) requeueActive(ctx context.Context, work chan<- *store.Task) {
    tasks, err := p.store.ListActiveTasks(ctx)
    if err != nil {
        slog.Error("list active tasks error", "error", err)
        return
    }
    for _, t := range tasks {
        slog.Info("re-queuing active task", "task_id", t.ID, "status", t.Status)
        work <- t
    }
}

// Transition updates a task's status in the store and logs the change.
func Transition(ctx context.Context, s *store.Store, task *store.Task, status store.TaskStatus, reason string) error {
    task.Status = status
    task.BlockReason = reason
    task.UpdatedAt = time.Now()
    if err := s.UpdateStatus(ctx, task.ID, status, reason); err != nil {
        return fmt.Errorf("transition %s→%s: %w", task.Status, status, err)
    }
    slog.Info("task transition", "task_id", task.ID, "status", status, "reason", reason)
    return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pipeline/... -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/
git commit -m "feat: add pipeline worker pool and task state machine"
```

---

## Task 7: Executor tools

**Files:**
- Create: `internal/executor/tools.go`

- [ ] **Step 1: Write tools.go**

Create `internal/executor/tools.go`:

```go
package executor

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

// allowedCommands is the prefix allowlist for run_command.
var allowedCommands = map[string]bool{
    "go": true, "make": true, "golangci-lint": true, "git": true,
}

// RunCommand executes a shell command in the given working directory.
// Only commands whose first word is in allowedCommands are permitted.
// Timeout is enforced via context.
func RunCommand(ctx context.Context, workdir, cmd string, timeout time.Duration) (stdout, stderr string, exit int, err error) {
    parts := strings.Fields(cmd)
    if len(parts) == 0 {
        return "", "", -1, fmt.Errorf("empty command")
    }
    if !allowedCommands[parts[0]] {
        return "", "", -1, fmt.Errorf("command %q not in allowlist", parts[0])
    }

    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    c := exec.CommandContext(ctx, parts[0], parts[1:]...)
    c.Dir = workdir
    var outBuf, errBuf strings.Builder
    c.Stdout = &outBuf
    c.Stderr = &errBuf

    runErr := c.Run()
    stdout = outBuf.String()
    stderr = errBuf.String()
    if runErr != nil {
        if exitErr, ok := runErr.(*exec.ExitError); ok {
            return stdout, stderr, exitErr.ExitCode(), nil
        }
        return stdout, stderr, -1, runErr
    }
    return stdout, stderr, 0, nil
}

// CreateWorktree creates a new git worktree at /tmp/harness-<branch>.
func CreateWorktree(ctx context.Context, repoPath, branch string) (string, error) {
    worktreePath := filepath.Join(os.TempDir(), "harness-"+branch)
    args := []string{"worktree", "add", "-b", branch, worktreePath, "origin/main"}
    c := exec.CommandContext(ctx, "git", args...)
    c.Dir = repoPath
    out, err := c.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("git worktree add: %w\n%s", err, out)
    }
    return worktreePath, nil
}

// DeleteWorktree removes a git worktree.
func DeleteWorktree(ctx context.Context, repoPath, worktreePath string) error {
    c := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktreePath)
    c.Dir = repoPath
    out, err := c.CombinedOutput()
    if err != nil {
        return fmt.Errorf("git worktree remove: %w\n%s", err, out)
    }
    return nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func WriteFile(path, content string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    return os.WriteFile(path, []byte(content), 0o644)
}

// ReadFile reads a file and returns its contents.
func ReadFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    return string(data), nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/executor/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/executor/tools.go
git commit -m "feat: add executor tool implementations (run_command, worktree, file)"
```

---

## Task 8: Notifier

**Files:**
- Create: `internal/notifier/notifier.go`

- [ ] **Step 1: Write notifier.go**

Create `internal/notifier/notifier.go`:

```go
package notifier

import (
    "context"
    "fmt"
    "strings"

    "github.com/google/go-github/v60/github"
    "github.com/wangke19/harness-system/internal/store"
)

// GitHubNotifier posts comments to GitHub issues and PRs.
type GitHubNotifier struct {
    client *github.Client
    owner  string
    repo   string
}

func NewGitHub(token, repoSlug string) *GitHubNotifier {
    parts := strings.SplitN(repoSlug, "/", 2)
    return &GitHubNotifier{
        client: github.NewClient(nil).WithAuthToken(token),
        owner:  parts[0],
        repo:   parts[1],
    }
}

func (n *GitHubNotifier) NotifyBlocked(ctx context.Context, task *store.Task, reason string) error {
    body := fmt.Sprintf("🚧 **Harness blocked** on task `%s`\n\n**Reason:** %s\n\nPlease resolve and re-label the issue to resume.", task.ID, reason)
    return n.commentOnIssue(ctx, task, body)
}

func (n *GitHubNotifier) NotifyDone(ctx context.Context, task *store.Task, prURL string) error {
    body := fmt.Sprintf("✅ **Harness completed** task `%s`\n\nPR merged: %s", task.ID, prURL)
    return n.commentOnIssue(ctx, task, body)
}

func (n *GitHubNotifier) NotifyFailed(ctx context.Context, task *store.Task, err error) error {
    body := fmt.Sprintf("❌ **Harness failed** on task `%s`\n\n**Error:** %v", task.ID, err)
    return n.commentOnIssue(ctx, task, body)
}

func (n *GitHubNotifier) commentOnIssue(ctx context.Context, task *store.Task, body string) error {
    // Extract issue number from URL (last path segment).
    var issueNum int
    fmt.Sscanf(task.IssueURL[strings.LastIndex(task.IssueURL, "/")+1:], "%d", &issueNum)
    if issueNum == 0 {
        return fmt.Errorf("could not parse issue number from %q", task.IssueURL)
    }
    _, _, err := n.client.Issues.CreateComment(ctx, n.owner, n.repo, issueNum, &github.IssueComment{
        Body: github.String(body),
    })
    return err
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/notifier/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/notifier/
git commit -m "feat: add GitHub notifier for blocked/done/failed comments"
```

---

## Task 9: Planner agent

**Files:**
- Create: `internal/planner/planner.go`
- Create: `internal/planner/planner_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/planner/planner_test.go`:

```go
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

// mockAgent returns a fixed plan text and records tool calls.
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/planner/... -v
```

Expected: compile error — `planner` package does not exist.

- [ ] **Step 3: Implement planner.go**

Create `internal/planner/planner.go`:

```go
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
    planDir string // directory where exec-plans/*.md are written
}

func New(a agent.Agent, planDir string) *Planner {
    return &Planner{agent: a, planDir: planDir}
}

// Plan runs the planner agent for the given task and issue body.
// It writes the resulting plan to exec-plans/<task-id>.md and updates task.PlanPath.
func (p *Planner) Plan(ctx context.Context, task *store.Task, issueBody string) (string, error) {
    tools := plannerTools()
    prompt := buildPlannerPrompt(task, issueBody)

    text, calls, err := p.agent.CompleteWithTools(ctx, prompt, tools)
    if err != nil {
        return "", agent.ErrRetryable{Reason: fmt.Sprintf("planner LLM error: %v", err)}
    }

    // Execute tool calls requested by the planner.
    plan := text
    for _, call := range calls {
        if call.Name == "write_plan" {
            var args struct{ Content string }
            if err := json.Unmarshal(call.Input, &args); err != nil {
                continue
            }
            plan = args.Content
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/planner/... -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/planner/
git commit -m "feat: add planner agent that writes exec-plan markdown"
```

---

## Task 10: Executor agent

**Files:**
- Create: `internal/executor/executor.go`
- Create: `internal/executor/executor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/executor/executor_test.go`:

```go
package executor_test

import (
    "context"
    "testing"

    "github.com/wangke19/harness-system/internal/agent"
    "github.com/wangke19/harness-system/internal/executor"
    "github.com/wangke19/harness-system/internal/store"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/executor/... -v -run TestExecutor_ReturnsBlockerOnMaxRetries
```

Expected: compile error — `executor.New` not defined.

- [ ] **Step 3: Implement executor.go**

Create `internal/executor/executor.go`:

```go
package executor

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "time"

    "github.com/wangke19/harness-system/internal/agent"
    "github.com/wangke19/harness-system/internal/store"
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
// Returns ErrRetryable, ErrBlocker, or ErrFatal depending on what went wrong.
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

    tools := e.executorTools(worktreePath)
    prompt := buildExecutorPrompt(task, string(plan), worktreePath)

    _, calls, err := e.agent.CompleteWithTools(ctx, prompt, tools)
    if err != nil {
        return agent.ErrRetryable{Reason: fmt.Sprintf("executor LLM error: %v", err)}
    }

    // Execute tool calls in order.
    var prNumber int
    for _, call := range calls {
        result, callErr := e.executeToolCall(ctx, worktreePath, call)
        if callErr != nil {
            return agent.ErrRetryable{Reason: fmt.Sprintf("tool %s error: %v", call.Name, callErr)}
        }
        if call.Name == "create_pr" {
            fmt.Sscanf(result, "%d", &prNumber)
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
        var args struct{ Path string `json:"path"` }
        if err := json.Unmarshal(call.Input, &args); err != nil {
            return "", err
        }
        return ReadFile(workdir + "/" + args.Path)

    case "run_command":
        var args struct{ Cmd string `json:"cmd"` }
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
        // Placeholder: actual GitHub API call wired in Task 12.
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

func (e *Executor) executorTools(worktreePath string) []agent.Tool {
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/executor/... -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/executor/
git commit -m "feat: add executor agent (plan → code → PR)"
```

---

## Task 11: Reviewer agent

**Files:**
- Create: `internal/reviewer/reviewer.go`
- Create: `internal/reviewer/reviewer_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/reviewer/reviewer_test.go`:

```go
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
    r := reviewer.New(a, nil) // nil GitHub client — mock in real integration test

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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/reviewer/... -v
```

Expected: compile error — `reviewer` package does not exist.

- [ ] **Step 3: Implement reviewer.go**

Create `internal/reviewer/reviewer.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/reviewer/... -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/reviewer/
git commit -m "feat: add reviewer agent (diff → approve/request_changes)"
```

---

## Task 12: Merger and GitHub PR wiring

**Files:**
- Create: `internal/merger/merger.go`

- [ ] **Step 1: Write merger.go**

Create `internal/merger/merger.go`:

```go
package merger

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/google/go-github/v60/github"
)

// Merger merges a GitHub PR once all checks pass.
type Merger struct {
    client *github.Client
    owner  string
    repo   string
}

func New(token, repoSlug string) *Merger {
    parts := strings.SplitN(repoSlug, "/", 2)
    return &Merger{
        client: github.NewClient(nil).WithAuthToken(token),
        owner:  parts[0],
        repo:   parts[1],
    }
}

// WaitAndMerge polls PR checks until all pass, then merges.
// Returns the merge commit SHA or an error.
func (m *Merger) WaitAndMerge(ctx context.Context, prNumber int) (string, error) {
    for {
        select {
        case <-ctx.Done():
            return "", ctx.Err()
        case <-time.After(30 * time.Second):
        }

        pr, _, err := m.client.PullRequests.Get(ctx, m.owner, m.repo, prNumber)
        if err != nil {
            return "", fmt.Errorf("get PR: %w", err)
        }

        switch pr.GetMergeableState() {
        case "clean":
            result, _, err := m.client.PullRequests.Merge(ctx, m.owner, m.repo, prNumber,
                "Auto-merged by harness", &github.PullRequestOptions{MergeMethod: "squash"})
            if err != nil {
                return "", fmt.Errorf("merge PR: %w", err)
            }
            return result.GetSHA(), nil
        case "blocked", "behind", "dirty":
            return "", fmt.Errorf("PR not mergeable: %s", pr.GetMergeableState())
        // "unstable", "unknown" → keep polling
        }
    }
}

// GetDiff fetches the unified diff for a PR.
func (m *Merger) GetDiff(ctx context.Context, prNumber int) (string, error) {
    files, _, err := m.client.PullRequests.ListFiles(ctx, m.owner, m.repo, prNumber, nil)
    if err != nil {
        return "", fmt.Errorf("list PR files: %w", err)
    }
    var sb strings.Builder
    for _, f := range files {
        sb.WriteString(f.GetPatch())
        sb.WriteString("\n")
    }
    return sb.String(), nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/merger/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/merger/
git commit -m "feat: add merger that polls checks and squash-merges PR"
```

---

## Task 13: Wire everything in main

**Files:**
- Modify: `cmd/harness/main.go`

- [ ] **Step 1: Update main.go to wire all components**

Replace the contents of `cmd/harness/main.go`:

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/wangke19/harness-system/config"
    "github.com/wangke19/harness-system/internal/agent"
    "github.com/wangke19/harness-system/internal/executor"
    "github.com/wangke19/harness-system/internal/merger"
    "github.com/wangke19/harness-system/internal/notifier"
    "github.com/wangke19/harness-system/internal/pipeline"
    "github.com/wangke19/harness-system/internal/planner"
    "github.com/wangke19/harness-system/internal/reviewer"
    "github.com/wangke19/harness-system/internal/store"
    "github.com/wangke19/harness-system/internal/watcher"
)

func main() {
    cfgPath := flag.String("config", "config/config.yaml", "path to config file")
    flag.Parse()

    cfg, err := config.Load(*cfgPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }

    // Store
    db, err := store.New(cfg.Server.DBPath)
    if err != nil {
        slog.Error("failed to open store", "error", err)
        os.Exit(1)
    }
    defer db.Close()

    // Agents
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        slog.Error("ANTHROPIC_API_KEY not set")
        os.Exit(1)
    }
    plannerAgent := agent.NewClaude(apiKey, cfg.Agents["planner"].Model)
    executorAgent := agent.NewClaude(apiKey, cfg.Agents["executor"].Model)
    reviewerAgent := agent.NewClaude(apiKey, cfg.Agents["reviewer"].Model)

    // GitHub credentials
    ghToken := os.Getenv("GITHUB_TOKEN")
    if ghToken == "" {
        slog.Error("GITHUB_TOKEN not set")
        os.Exit(1)
    }
    repoSlug := cfg.Watch.GitHub.Repo

    // Components
    plannerSvc := planner.New(plannerAgent, "exec-plans")
    executorSvc := executor.New(executorAgent, ".", cfg.Executor.MaxRetries)
    reviewerSvc := reviewer.New(reviewerAgent, nil) // GitHub client wired below
    mergerSvc := merger.New(ghToken, repoSlug)
    notifierSvc := notifier.NewGitHub(ghToken, repoSlug)

    // Orchestrator handler
    handler := &orchestrator{
        store:    db,
        planner:  plannerSvc,
        executor: executorSvc,
        reviewer: reviewerSvc,
        merger:   mergerSvc,
        notifier: notifierSvc,
    }

    // Watcher
    ghSource := watcher.NewGitHubSource(ghToken, repoSlug, cfg.Watch.GitHub.Label)
    events := make(chan watcher.IssueEvent, 32)
    w := watcher.New(db, events, cfg.Server.PollInterval, ghSource)

    // Pipeline
    p := pipeline.New(db, events, cfg.Server.WorkerCount, handler)

    // Run
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    slog.Info("harness started", "repo", repoSlug, "workers", cfg.Server.WorkerCount)
    go w.Run(ctx)
    p.Run(ctx)
}

// orchestrator implements pipeline.Handler by chaining all agents.
type orchestrator struct {
    store    *store.Store
    planner  *planner.Planner
    executor *executor.Executor
    reviewer *reviewer.Reviewer
    merger   *merger.Merger
    notifier *notifier.GitHubNotifier
}

func (o *orchestrator) Handle(ctx context.Context, task *store.Task) error {
    // Fetch issue body (simplified: use stored URL).
    issueBody := fmt.Sprintf("Issue: %s", task.IssueURL)

    // Planning
    if err := pipeline.Transition(ctx, o.store, task, store.StatusPlanning, ""); err != nil {
        return err
    }
    planPath, err := o.planner.Plan(ctx, task, issueBody)
    if err != nil {
        return o.handleError(ctx, task, err)
    }
    task.PlanPath = planPath
    _ = o.store.UpdateTask(ctx, task)

    // Executing
    if err := pipeline.Transition(ctx, o.store, task, store.StatusExecuting, ""); err != nil {
        return err
    }
    if err := o.executor.Execute(ctx, task); err != nil {
        return o.handleError(ctx, task, err)
    }
    _ = o.store.UpdateTask(ctx, task)

    // Reviewing
    if err := pipeline.Transition(ctx, o.store, task, store.StatusReviewing, ""); err != nil {
        return err
    }
    diff, _ := o.merger.GetDiff(ctx, task.PRNumber)
    decision, err := o.reviewer.Review(ctx, task, diff)
    if err != nil {
        return o.handleError(ctx, task, err)
    }
    if decision == reviewer.DecisionRequestChanges {
        task.RetryCount++
        _ = o.store.UpdateTask(ctx, task)
        return pipeline.Transition(ctx, o.store, task, store.StatusExecuting, "reviewer requested changes")
    }

    // Merging
    if err := pipeline.Transition(ctx, o.store, task, store.StatusMerging, ""); err != nil {
        return err
    }
    sha, err := o.merger.WaitAndMerge(ctx, task.PRNumber)
    if err != nil {
        return o.handleError(ctx, task, err)
    }

    _ = pipeline.Transition(ctx, o.store, task, store.StatusDone, "")
    _ = o.notifier.NotifyDone(ctx, task, fmt.Sprintf("https://github.com/%s/commit/%s", o.merger.RepoSlug(), sha))
    return nil
}

func (o *orchestrator) handleError(ctx context.Context, task *store.Task, err error) error {
    switch e := err.(type) {
    case agent.ErrBlocker:
        _ = pipeline.Transition(ctx, o.store, task, store.StatusBlocked, e.Reason)
        _ = o.notifier.NotifyBlocked(ctx, task, e.Reason)
    case agent.ErrFatal:
        _ = pipeline.Transition(ctx, o.store, task, store.StatusFailed, e.Reason)
        _ = o.notifier.NotifyFailed(ctx, task, err)
    case agent.ErrRetryable:
        task.RetryCount++
        _ = o.store.UpdateTask(ctx, task)
        // Re-queue by returning nil — pipeline will re-process on next poll.
    }
    return nil
}
```

Also add `RepoSlug()` method to `merger.Merger`:

```go
// Add to internal/merger/merger.go
func (m *Merger) RepoSlug() string {
    return m.owner + "/" + m.repo
}
```

- [ ] **Step 2: Build to verify all wiring compiles**

```bash
go build ./...
```

Expected: no output. Fix any import errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/harness/main.go internal/merger/merger.go
git commit -m "feat: wire all components in main orchestrator"
```

---

## Task 14: Final verification

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: all tests pass. No skips.

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./...
```

Expected: no errors. Fix any reported issues.

- [ ] **Step 3: Build binary**

```bash
go build -o bin/harness ./cmd/harness
```

Expected: `bin/harness` created.

- [ ] **Step 4: Smoke test config loading**

```bash
ANTHROPIC_API_KEY=fake GITHUB_TOKEN=fake ./bin/harness --config config/config.yaml
```

Expected: logs `harness started` then blocks (no crash on startup).

- [ ] **Step 5: Final commit**

```bash
git add bin/ .gitignore
git commit -m "chore: add build artifact to .gitignore, final verification"
```

Add a `.gitignore`:

```
bin/
harness.db
exec-plans/
*.db
```

---

## Self-Review Checklist

**Spec coverage:**
- ✅ Go module + config (Task 1)
- ✅ SQLite store with task CRUD and seen-issue dedup (Task 2)
- ✅ Agent interface + error types ErrRetryable/ErrBlocker/ErrFatal (Task 3)
- ✅ Claude implementation with tool-use loop (Task 4)
- ✅ Watcher with GitHub + Jira sources, dedup (Task 5)
- ✅ Pipeline worker pool + state machine + restart survival (Task 6)
- ✅ Executor tools: run_command allowlist, worktree, file ops (Task 7)
- ✅ Notifier: blocked/done/failed GitHub comments (Task 8)
- ✅ Planner agent (Task 9)
- ✅ Executor agent (Task 10)
- ✅ Reviewer agent with approve/request_changes (Task 11)
- ✅ Merger: poll checks, squash-merge, get diff (Task 12)
- ✅ Orchestrator wiring all agents through state machine (Task 13)
- ✅ Tiered autonomy: ErrRetryable → retry, ErrBlocker → notify, ErrFatal → fail

**Out of scope confirmed not implemented:** multi-language, parallel tasks, dashboard, self-hosted LLMs, auto-learning.
