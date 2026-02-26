package azuredevops

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

// Client is an Azure DevOps REST API client that implements tracker.Provider.
type Client struct {
	baseURL string
	org     string
	token   string
	http    tracker.HTTPDoer
}

// New creates an Azure DevOps client with the given base URL, organization, and PAT.
func New(baseURL, org, token string) *Client {
	return &Client{baseURL: baseURL, org: org, token: token, http: http.DefaultClient}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer tracker.HTTPDoer) {
	c.http = doer
}

// ListIssues implements tracker.Lister using WIQL to query work items.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	project := opts.Project
	if project == "" {
		return nil, errors.WithDetails("project is required for Azure DevOps")
	}

	query := fmt.Sprintf(
		"SELECT [System.Id] FROM workitems WHERE [System.TeamProject] = '%s'",
		project,
	)
	if !opts.IncludeAll {
		query += " AND [System.State] <> 'Done' AND [System.State] <> 'Removed'"
	}

	wiqlBody, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling WIQL query", "project", project)
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/wiql", c.org, project)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "api-version=7.1", bytes.NewReader(wiqlBody), "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var wiqlResp adoWIQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&wiqlResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding WIQL response", "project", project)
	}

	if len(wiqlResp.WorkItems) == 0 {
		return nil, nil
	}

	ids := make([]string, len(wiqlResp.WorkItems))
	for i, ref := range wiqlResp.WorkItems {
		ids[i] = strconv.Itoa(ref.ID)
	}

	batchPath := fmt.Sprintf("/%s/%s/_apis/wit/workitems", c.org, project)
	batchQuery := url.Values{
		"ids":         {strings.Join(ids, ",")},
		"api-version": {"7.1"},
	}
	batchResp, err := c.doRequest(ctx, http.MethodGet, batchPath, batchQuery.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = batchResp.Body.Close() }()

	var batchResult struct {
		Value []adoWorkItem `json:"value"`
	}
	if err := json.NewDecoder(batchResp.Body).Decode(&batchResult); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding batch work items", "project", project)
	}

	issues := make([]tracker.Issue, 0, len(batchResult.Value))
	for _, wi := range batchResult.Value {
		issues = append(issues, toTrackerIssue(wi, project))
	}
	return issues, nil
}

// GetIssue implements tracker.Getter.
func (c *Client) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	project, id, err := parseIssueKey(key)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/%d", c.org, project, id)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "api-version=7.1", nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var wi adoWorkItem
	if err := json.NewDecoder(resp.Body).Decode(&wi); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding work item", "key", key)
	}

	issue := toTrackerIssue(wi, project)
	return &issue, nil
}

// CreateIssue implements tracker.Creator using JSON Patch format.
func (c *Client) CreateIssue(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
	project := issue.Project
	if project == "" {
		return nil, errors.WithDetails("project is required for Azure DevOps")
	}

	ops := []patchOp{
		{Op: "add", Path: "/fields/System.Title", Value: issue.Summary},
	}
	if issue.Description != "" {
		ops = append(ops, patchOp{Op: "add", Path: "/fields/System.Description", Value: issue.Description})
	}

	body, err := json.Marshal(ops)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling create request", "project", project)
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/$Issue", c.org, project)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "api-version=7.1", bytes.NewReader(body), "application/json-patch+json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var wi adoWorkItem
	if err := json.NewDecoder(resp.Body).Decode(&wi); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding create response", "project", project)
	}

	return &tracker.Issue{
		Key:         fmt.Sprintf("%s/%d", project, wi.ID),
		Project:     project,
		Summary:     wi.Fields.Title,
		Description: wi.Fields.Description,
	}, nil
}

// DeleteIssue implements tracker.Deleter by transitioning the work item to "Done".
// Azure DevOps does not support true deletion via the REST API.
func (c *Client) DeleteIssue(ctx context.Context, key string) error {
	project, id, err := parseIssueKey(key)
	if err != nil {
		return err
	}

	ops := []patchOp{
		{Op: "add", Path: "/fields/System.State", Value: "Done"},
	}
	body, err := json.Marshal(ops)
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling delete request", "key", key)
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/%d", c.org, project, id)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "api-version=7.1", bytes.NewReader(body), "application/json-patch+json")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// AddComment implements tracker.Commenter.
func (c *Client) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	project, id, err := parseIssueKey(issueKey)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"text": body})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling comment request", "issueKey", issueKey)
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/workItems/%d/comments", c.org, project, id)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "api-version=7.1-preview.4", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var ac adoComment
	if err := json.NewDecoder(resp.Body).Decode(&ac); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comment response", "issueKey", issueKey)
	}

	return toTrackerComment(ac)
}

// ListComments implements tracker.Commenter.
func (c *Client) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	project, id, err := parseIssueKey(issueKey)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/%s/%s/_apis/wit/workItems/%d/comments", c.org, project, id)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "api-version=7.1-preview.4", nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var cl adoCommentList
	if err := json.NewDecoder(resp.Body).Decode(&cl); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comments response", "issueKey", issueKey)
	}

	comments := make([]tracker.Comment, 0, len(cl.Comments))
	for _, ac := range cl.Comments {
		tc, err := toTrackerComment(ac)
		if err != nil {
			return nil, err
		}
		comments = append(comments, *tc)
	}
	return comments, nil
}

func toTrackerComment(ac adoComment) (*tracker.Comment, error) {
	created, err := time.Parse(time.RFC3339, ac.CreatedDate)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing comment timestamp", "commentID", ac.ID)
	}

	author := ""
	if ac.CreatedBy != nil {
		author = ac.CreatedBy.DisplayName
	}

	return &tracker.Comment{
		ID:      strconv.Itoa(ac.ID),
		Author:  author,
		Body:    ac.Text,
		Created: created,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, rawQuery string, body io.Reader, contentType string) (*http.Response, error) {
	if err := tracker.ValidateURL(c.baseURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing base URL", "baseURL", c.baseURL)
	}
	u.Path = path
	u.RawQuery = rawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request", "method", method, "path", path)
	}
	req.SetBasicAuth("", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting Azure DevOps",
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting Azure DevOps: nil response",
			"method", method, "path", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, errors.WithDetails(
			fmt.Sprintf("azuredevops %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody)),
			"statusCode", resp.StatusCode, "method", method, "path", path)
	}
	return resp, nil
}

// parseIssueKey parses a "Project/ID" key into project name and numeric ID.
func parseIssueKey(key string) (string, int, error) {
	slashIdx := strings.LastIndex(key, "/")
	if slashIdx < 0 {
		return "", 0, errors.WithDetails("invalid issue key format, expected Project/ID",
			"key", key)
	}

	project := key[:slashIdx]
	if project == "" {
		return "", 0, errors.WithDetails("invalid issue key format, expected Project/ID",
			"key", key)
	}

	idStr := key[slashIdx+1:]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return "", 0, errors.WithDetails("invalid work item ID in key",
			"key", key)
	}

	return project, id, nil
}

// toTrackerIssue converts an Azure DevOps work item to a tracker.Issue.
func toTrackerIssue(wi adoWorkItem, project string) tracker.Issue {
	issue := tracker.Issue{
		Key:         fmt.Sprintf("%s/%d", project, wi.ID),
		Project:     project,
		Type:        wi.Fields.WorkItemType,
		Summary:     wi.Fields.Title,
		Status:      wi.Fields.State,
		Description: wi.Fields.Description,
	}

	if wi.Fields.Priority > 0 {
		issue.Priority = strconv.Itoa(wi.Fields.Priority)
	}
	if wi.Fields.AssignedTo != nil {
		issue.Assignee = wi.Fields.AssignedTo.DisplayName
	}
	if wi.Fields.CreatedBy != nil {
		issue.Reporter = wi.Fields.CreatedBy.DisplayName
	}

	return issue
}
