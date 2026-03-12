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

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/tracker"
)

var _ tracker.Provider = (*Client)(nil)

// Client is a GitHub REST API client that implements tracker.Lister,
// tracker.Getter, and tracker.Creator.
type Client struct {
	baseURL string
	token   string
	http    tracker.HTTPDoer
}

// New creates a GitHub client with the given base URL and personal access token.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: http.DefaultClient}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer tracker.HTTPDoer) {
	c.http = doer
}

// ListIssues implements tracker.Lister.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	owner, repo, err := splitProject(opts.Project)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	state := "open"
	if opts.IncludeAll {
		state = "all"
	}
	query := url.Values{
		"per_page": {fmt.Sprintf("%d", opts.MaxResults)},
		"state":    {state},
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
		Title: issue.Title,
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
		Title:       result.Title,
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

// ListStatuses implements tracker.StatusLister.
// GitHub issues have fixed states: open and closed.
func (c *Client) ListStatuses(_ context.Context, _ string) ([]tracker.Status, error) {
	return []tracker.Status{
		{Name: "open", Type: "started"},
		{Name: "closed", Type: "closed"},
	}, nil
}

// TransitionIssue implements tracker.Transitioner.
func (c *Client) TransitionIssue(ctx context.Context, key string, targetStatus string) error {
	lower := strings.ToLower(targetStatus)
	if lower != "open" && lower != "closed" {
		return errors.WithDetails("GitHub only supports 'open' and 'closed' states",
			"key", key, "targetStatus", targetStatus)
	}

	owner, repo, number, err := parseIssueKey(key)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]string{"state": lower})
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling transition request", "key", key)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// AssignIssue implements tracker.Assigner.
func (c *Client) AssignIssue(ctx context.Context, key string, userID string) error {
	owner, repo, number, err := parseIssueKey(key)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string][]string{"assignees": {userID}})
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling assign request", "key", key)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetCurrentUser implements tracker.CurrentUserGetter.
func (c *Client) GetCurrentUser(ctx context.Context) (string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/user", "", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var user ghCurrentUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", errors.WrapWithDetails(err, "decoding current user response")
	}
	return user.Login, nil
}

// EditIssue implements tracker.Editor.
func (c *Client) EditIssue(ctx context.Context, key string, opts tracker.EditOptions) (*tracker.Issue, error) {
	owner, repo, number, err := parseIssueKey(key)
	if err != nil {
		return nil, err
	}

	fields := make(map[string]string)
	if opts.Title != nil {
		fields["title"] = *opts.Title
	}
	if opts.Description != nil {
		fields["body"] = *opts.Description
	}

	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling edit request", "key", key)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var gi ghIssue
	if err := json.NewDecoder(resp.Body).Decode(&gi); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding edit response", "key", key)
	}

	issue := toTrackerIssue(owner, repo, gi)
	return &issue, nil
}

// DeleteIssue implements tracker.Deleter by closing the issue.
// GitHub does not support true deletion via the API, so we close the issue instead.
func (c *Client) DeleteIssue(ctx context.Context, key string) error {
	owner, repo, number, err := parseIssueKey(key)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]string{"state": "closed"})
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling delete request", "key", key)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path, rawQuery string, body io.Reader) (*http.Response, error) {
	if err := tracker.ValidateURL(c.baseURL); err != nil {
		return nil, err
	}
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

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting GitHub",
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting GitHub: nil response",
			"method", method, "path", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, errors.WithDetails(
			fmt.Sprintf("github %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody)),
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
		Title:       gi.Title,
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
