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
	baseURL    string
	token      string
	project    string
	status     string
	httpClient *http.Client
}

func NewJiraSource(baseURL, token, project, status string) *JiraSource {
	return &JiraSource{
		baseURL:    baseURL,
		token:      token,
		project:    project,
		status:     status,
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
			Fields struct {
				Summary string `json:"summary"`
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
