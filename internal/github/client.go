package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/tracker"
)

var _ tracker.Provider = (*Client)(nil)

// Client is a GitHub REST API client that implements tracker.Lister,
// tracker.Getter, and tracker.Creator.
type Client struct {
	baseURL string
	token   string
}

// New creates a GitHub client with the given base URL and personal access token.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token}
}

// ListIssues implements tracker.Lister.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	owner, repo, err := splitProject(opts.Project)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	query := url.Values{
		"per_page": {fmt.Sprintf("%d", opts.MaxResults)},
		"state":    {"open"},
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var ghIssues []ghIssue
	if err := json.NewDecoder(resp.Body).Decode(&ghIssues); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding response",
			"project", opts.Project)
	}

	var issues []tracker.Issue
	for _, gi := range ghIssues {
		if gi.PullRequest != nil {
			continue
		}
		issues = append(issues, toTrackerIssue(owner, repo, gi))
	}
	return issues, nil
}

// GetIssue implements tracker.Getter.
func (c *Client) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	owner, repo, number, err := parseIssueKey(key)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var gi ghIssue
	if err := json.NewDecoder(resp.Body).Decode(&gi); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding response",
			"issueKey", key)
	}

	issue := toTrackerIssue(owner, repo, gi)
	return &issue, nil
}

// CreateIssue implements tracker.Creator.
func (c *Client) CreateIssue(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
	owner, repo, err := splitProject(issue.Project)
	if err != nil {
		return nil, err
	}

	payload := createRequest{
		Title: issue.Summary,
		Body:  issue.Description,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling create request",
			"project", issue.Project)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result createResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding create response",
			"project", issue.Project)
	}

	return &tracker.Issue{
		Key:         fmt.Sprintf("%s/%s#%d", owner, repo, result.Number),
		Project:     issue.Project,
		Summary:     result.Title,
		Description: result.Body,
	}, nil
}

// AddComment implements tracker.Commenter.
func (c *Client) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	owner, repo, number, err := parseIssueKey(issueKey)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(commentRequest{Body: body})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling comment request",
			"issueKey", issueKey)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var gc ghComment
	if err := json.NewDecoder(resp.Body).Decode(&gc); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comment response",
			"issueKey", issueKey)
	}

	return toTrackerComment(gc)
}

// ListComments implements tracker.Commenter.
func (c *Client) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	owner, repo, number, err := parseIssueKey(issueKey)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var ghComments []ghComment
	if err := json.NewDecoder(resp.Body).Decode(&ghComments); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comments response",
			"issueKey", issueKey)
	}

	comments := make([]tracker.Comment, 0, len(ghComments))
	for _, gc := range ghComments {
		c, err := toTrackerComment(gc)
		if err != nil {
			return nil, err
		}
		comments = append(comments, *c)
	}
	return comments, nil
}

func toTrackerComment(gc ghComment) (*tracker.Comment, error) {
	created, err := time.Parse(time.RFC3339, gc.CreatedAt)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing comment timestamp",
			"commentID", gc.ID)
	}

	author := ""
	if gc.User != nil {
		author = gc.User.Login
	}

	return &tracker.Comment{
		ID:      strconv.Itoa(gc.ID),
		Author:  author,
		Body:    gc.Body,
		Created: created,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, rawQuery string, body io.Reader) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing base URL",
			"baseURL", c.baseURL)
	}
	u.Path = path
	u.RawQuery = rawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"method", method, "path", path)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting GitHub",
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting GitHub: nil response",
			"method", method, "path", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, errors.WithDetails("github returned unexpected status",
			"statusCode", resp.StatusCode, "method", method, "path", path)
	}
	return resp, nil
}

// splitProject parses an "owner/repo" string.
func splitProject(project string) (string, string, error) {
	parts := strings.SplitN(project, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.WithDetails("invalid project format, expected owner/repo",
			"project", project)
	}
	return parts[0], parts[1], nil
}

// parseIssueKey parses an "owner/repo#123" key into its components.
func parseIssueKey(key string) (string, string, int, error) {
	hashIdx := strings.LastIndex(key, "#")
	if hashIdx < 0 {
		return "", "", 0, errors.WithDetails("invalid issue key format, expected owner/repo#number",
			"key", key)
	}

	project := key[:hashIdx]
	numberStr := key[hashIdx+1:]

	owner, repo, err := splitProject(project)
	if err != nil {
		return "", "", 0, errors.WithDetails("invalid issue key format, expected owner/repo#number",
			"key", key)
	}

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return "", "", 0, errors.WithDetails("invalid issue number in key",
			"key", key)
	}

	return owner, repo, number, nil
}

// toTrackerIssue converts a GitHub API issue to a tracker.Issue.
func toTrackerIssue(owner, repo string, gi ghIssue) tracker.Issue {
	issue := tracker.Issue{
		Key:         fmt.Sprintf("%s/%s#%d", owner, repo, gi.Number),
		Summary:     gi.Title,
		Status:      gi.State,
		Description: gi.Body,
	}

	if len(gi.Labels) > 0 {
		issue.Type = gi.Labels[0].Name
	}
	if gi.Assignee != nil {
		issue.Assignee = gi.Assignee.Login
	}
	if gi.User != nil {
		issue.Reporter = gi.User.Login
	}

	return issue
}
