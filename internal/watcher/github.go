package watcher

import (
	"context"
	"fmt"
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
