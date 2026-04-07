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
	var issueNum int
	_, _ = fmt.Sscanf(task.IssueURL[strings.LastIndex(task.IssueURL, "/")+1:], "%d", &issueNum)
	if issueNum == 0 {
		return fmt.Errorf("could not parse issue number from %q", task.IssueURL)
	}
	_, _, err := n.client.Issues.CreateComment(ctx, n.owner, n.repo, issueNum, &github.IssueComment{
		Body: github.String(body),
	})
	return err
}
