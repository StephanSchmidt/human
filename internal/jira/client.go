package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"human/errors"
	"human/internal/jira/adf"
	"human/tracker"
)

// Client is a Jira REST API client that implements tracker.Lister and tracker.Getter.
type Client struct {
	baseURL string
	user    string
	key     string
}

// New creates a Jira client with the given base URL, user email, and API key.
func New(baseURL, user, key string) *Client {
	return &Client{baseURL: baseURL, user: user, key: key}
}

// ListIssues implements tracker.Lister.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	jql := fmt.Sprintf("project=%s order by created DESC", opts.Project)
	query := url.Values{
		"jql":        {jql},
		"maxResults": {fmt.Sprintf("%d", opts.MaxResults)},
		"fields":     {"*navigable"},
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/rest/api/3/search/jql", query.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result searchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding response",
			"project", opts.Project)
	}

	issues := make([]tracker.Issue, len(result.Issues))
	for i, iss := range result.Issues {
		issues[i] = tracker.Issue{
			Key:     iss.Key,
			Summary: iss.Fields.Summary,
			Status:  iss.Fields.Status.Name,
		}
	}
	return issues, nil
}

// GetIssue implements tracker.Getter.
func (c *Client) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	path := fmt.Sprintf("/rest/api/3/issue/%s", url.PathEscape(key))
	query := url.Values{
		"fields": {"summary,status,description,assignee,reporter,priority"},
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, query.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var detail issueDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding response",
			"issueKey", key)
	}

	f := detail.Fields
	desc := ""
	if hasDescription(f.Description) {
		var doc map[string]any
		if err := json.Unmarshal(f.Description, &doc); err != nil {
			return nil, errors.WrapWithDetails(err, "parsing description ADF",
				"issueKey", key)
		}
		desc = adf.ToMarkdown(doc)
	}

	return &tracker.Issue{
		Key:         detail.Key,
		Summary:     f.Summary,
		Status:      f.Status.Name,
		Priority:    nameOrEmpty(f.Priority),
		Assignee:    nameOrEmpty(f.Assignee),
		Reporter:    nameOrEmpty(f.Reporter),
		Description: desc,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, rawQuery string) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing base URL",
			"baseURL", c.baseURL)
	}
	u.Path = path
	u.RawQuery = rawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"method", method, "path", path)
	}
	req.SetBasicAuth(c.user, c.key)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting Jira",
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting Jira: nil response",
			"method", method, "path", path)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errors.WithDetails("jira returned unexpected status",
			"statusCode", resp.StatusCode, "method", method, "path", path)
	}
	return resp, nil
}

func hasDescription(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func nameOrEmpty(f *nameField) string {
	if f == nil {
		return ""
	}
	if f.DisplayName != "" {
		return f.DisplayName
	}
	return f.Name
}
