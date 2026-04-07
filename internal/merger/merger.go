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

// RepoSlug returns "owner/repo".
func (m *Merger) RepoSlug() string {
	return m.owner + "/" + m.repo
}

// WaitAndMerge polls PR checks until all pass, then squash-merges.
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

// GetDiff fetches the patch text for all files changed in a PR.
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
