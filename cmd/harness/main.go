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

	db, err := store.New(cfg.Server.DBPath)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		slog.Error("ANTHROPIC_API_KEY not set")
		os.Exit(1)
	}
	plannerAgent := agent.NewClaude(apiKey, cfg.Agents["planner"].Model)
	executorAgent := agent.NewClaude(apiKey, cfg.Agents["executor"].Model)
	reviewerAgent := agent.NewClaude(apiKey, cfg.Agents["reviewer"].Model)

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		slog.Error("GITHUB_TOKEN not set")
		os.Exit(1)
	}
	repoSlug := cfg.Watch.GitHub.Repo

	plannerSvc := planner.New(plannerAgent, "exec-plans")
	executorSvc := executor.New(executorAgent, ".", cfg.Executor.MaxRetries)
	reviewerSvc := reviewer.New(reviewerAgent, nil)
	mergerSvc := merger.New(ghToken, repoSlug)
	notifierSvc := notifier.NewGitHub(ghToken, repoSlug)

	handler := &orchestrator{
		store:    db,
		planner:  plannerSvc,
		executor: executorSvc,
		reviewer: reviewerSvc,
		merger:   mergerSvc,
		notifier: notifierSvc,
	}

	ghSource := watcher.NewGitHubSource(ghToken, repoSlug, cfg.Watch.GitHub.Label)
	events := make(chan watcher.IssueEvent, 32)
	w := watcher.New(db, events, cfg.Server.PollInterval, ghSource)

	p := pipeline.New(db, events, cfg.Server.WorkerCount, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("harness started", "repo", repoSlug, "workers", cfg.Server.WorkerCount)
	go w.Run(ctx)
	p.Run(ctx)
}

type orchestrator struct {
	store    *store.Store
	planner  *planner.Planner
	executor *executor.Executor
	reviewer *reviewer.Reviewer
	merger   *merger.Merger
	notifier *notifier.GitHubNotifier
}

func (o *orchestrator) Handle(ctx context.Context, task *store.Task) error {
	issueBody := fmt.Sprintf("Issue: %s", task.IssueURL)

	if err := pipeline.Transition(ctx, o.store, task, store.StatusPlanning, ""); err != nil {
		return err
	}
	planPath, err := o.planner.Plan(ctx, task, issueBody)
	if err != nil {
		return o.handleError(ctx, task, err)
	}
	task.PlanPath = planPath
	_ = o.store.UpdateTask(ctx, task)

	if err := pipeline.Transition(ctx, o.store, task, store.StatusExecuting, ""); err != nil {
		return err
	}
	if err := o.executor.Execute(ctx, task); err != nil {
		return o.handleError(ctx, task, err)
	}
	_ = o.store.UpdateTask(ctx, task)

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
	}
	return nil
}
