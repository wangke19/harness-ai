# Harness AI

An autonomous software development agent written in Go. It monitors GitHub and Jira for issues, then plans, implements, reviews, and merges code — without human intervention.

## How it works

```
Issue labeled "harness-ready"
    → Planner writes an implementation plan
    → Executor creates a branch, writes code, opens a PR
    → Reviewer checks the diff and test results
    → Merger squash-merges when CI is green
    → Notifier comments on the issue when done (or blocked)
```

Three Claude models power the pipeline:

| Agent    | Model                | Role                              |
|----------|----------------------|-----------------------------------|
| Planner  | claude-sonnet-4-6    | Analyze issue → write exec plan   |
| Executor | claude-sonnet-4-5    | Implement plan → open PR          |
| Reviewer | claude-opus-4-6      | Review diff → approve or reject   |

When something goes wrong (test failures, missing credentials, ambiguous spec), the system pauses and posts a comment explaining why, rather than guessing.

## Requirements

- Go 1.22+
- `gcc` (for SQLite via cgo)
- `git`
- `golangci-lint` (optional, used by the executor)

## Setup

```bash
git clone https://github.com/wangke19/harness-ai
cd harness-ai

# Edit config/config.yaml with your repo and label
cp config/config.yaml config/config.yaml

go build -o bin/harness ./cmd/harness
```

## Configuration

Edit `config/config.yaml`:

```yaml
watch:
  github:
    repo: owner/repo        # the repo to watch
    label: harness-ready    # label that triggers the agent

agents:
  planner:
    model: claude-sonnet-4-6
  executor:
    model: claude-sonnet-4-5
  reviewer:
    model: claude-opus-4-6
```

## Running

```bash
export ANTHROPIC_API_KEY=your-key
export GITHUB_TOKEN=your-token

./bin/harness --config config/config.yaml
```

The agent polls for new issues every 60 seconds. To trigger it, open a GitHub issue in the watched repo and apply the `harness-ready` label.

## Project layout

```
cmd/harness/       entry point
config/            YAML config
internal/
  agent/           Claude API client
  watcher/         GitHub / Jira polling
  pipeline/        Worker pool + task state machine
  store/           SQLite (task state, dedup)
  planner/         Issue → implementation plan
  executor/        Plan → code → PR
  reviewer/        PR diff → approve / request changes
  merger/          Squash merge when CI passes
  notifier/        GitHub comments on block / done / fail
```

## Design

See [ARCHITECTURE.md](ARCHITECTURE.md) for a full description of the state machine, agent tool sets, error handling, and configuration reference.
