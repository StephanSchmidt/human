package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"human/errors"
	"human/internal/tracker"
)

var _ tracker.Provider = (*Client)(nil)

const listIssuesQuery = `query($teamKey: String!, $first: Int!) {
	issues(filter: { team: { key: { eq: $teamKey } } }, first: $first, orderBy: createdAt) {
		nodes { identifier title description state { name } priorityLabel
			assignee { name } creator { name } labels { nodes { name } } }
	}
}`

const getIssueQuery = `query($id: String!) {
	issue(id: $id) {
		identifier title description state { name } priorityLabel
		assignee { name } creator { name } labels { nodes { name } }
	}
}`

const getTeamIDQuery = `query($key: String!) {
	teams(filter: { key: { eq: $key } }) { nodes { id } }
}`

const createIssueMutation = `mutation($teamId: String!, $title: String!, $description: String) {
	issueCreate(input: { teamId: $teamId, title: $title, description: $description }) {
		success
		issue { identifier title description }
	}
}`

// Client is a Linear GraphQL API client that implements tracker.Provider.
type Client struct {
	baseURL string
	token   string
}

// New creates a Linear client with the given base URL and API key.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token}
}

// ListIssues implements tracker.Lister.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	vars := map[string]any{
		"teamKey": opts.Project,
		"first":   opts.MaxResults,
	}

	data, err := c.doGraphQL(ctx, listIssuesQuery, vars)
	if err != nil {
		return nil, err
	}

	var result issuesData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding issues response",
			"project", opts.Project)
	}

	issues := make([]tracker.Issue, len(result.Issues.Nodes))
	for i, li := range result.Issues.Nodes {
		issues[i] = toTrackerIssue(li, opts.Project)
	}
	return issues, nil
}

// GetIssue implements tracker.Getter.
func (c *Client) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	vars := map[string]any{"id": key}

	data, err := c.doGraphQL(ctx, getIssueQuery, vars)
	if err != nil {
		return nil, err
	}

	var result issueData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding issue response",
			"issueKey", key)
	}

	issue := toTrackerIssue(result.Issue, projectFromIdentifier(result.Issue.Identifier))
	return &issue, nil
}

// CreateIssue implements tracker.Creator.
func (c *Client) CreateIssue(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
	teamID, err := c.resolveTeamID(ctx, issue.Project)
	if err != nil {
		return nil, err
	}

	vars := map[string]any{
		"teamId": teamID,
		"title":  issue.Summary,
	}
	if issue.Description != "" {
		vars["description"] = issue.Description
	}

	data, err := c.doGraphQL(ctx, createIssueMutation, vars)
	if err != nil {
		return nil, err
	}

	var result issueCreateData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding create response",
			"project", issue.Project)
	}

	if !result.IssueCreate.Success {
		return nil, errors.WithDetails("linear issue creation failed",
			"project", issue.Project)
	}

	created := toTrackerIssue(result.IssueCreate.Issue, issue.Project)
	return &created, nil
}

// resolveTeamID looks up the internal Linear team ID for a team key.
func (c *Client) resolveTeamID(ctx context.Context, teamKey string) (string, error) {
	vars := map[string]any{"key": teamKey}

	data, err := c.doGraphQL(ctx, getTeamIDQuery, vars)
	if err != nil {
		return "", err
	}

	var result teamsData
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.WrapWithDetails(err, "decoding teams response",
			"teamKey", teamKey)
	}

	if len(result.Teams.Nodes) == 0 {
		return "", errors.WithDetails("linear team not found",
			"teamKey", teamKey)
	}

	return result.Teams.Nodes[0].ID, nil
}

// doGraphQL posts a GraphQL query to the Linear API and returns the data field.
func (c *Client) doGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	reqBody, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling graphql request")
	}

	endpoint := c.baseURL + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"endpoint", endpoint)
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting linear",
			"endpoint", endpoint)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting linear: nil response",
			"endpoint", endpoint)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.WithDetails("linear returned unexpected status",
			"statusCode", resp.StatusCode, "endpoint", endpoint)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "reading response body",
			"endpoint", endpoint)
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding graphql response",
			"endpoint", endpoint)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, errors.WithDetails(
			fmt.Sprintf("linear graphql error: %s", strings.Join(msgs, "; ")),
			"endpoint", endpoint)
	}

	return gqlResp.Data, nil
}

// toTrackerIssue converts a Linear API issue to a tracker.Issue.
func toTrackerIssue(li linearIssue, project string) tracker.Issue {
	issue := tracker.Issue{
		Key:         li.Identifier,
		Project:     project,
		Summary:     li.Title,
		Status:      li.State.Name,
		Priority:    li.PriorityLabel,
		Description: li.Description,
	}

	if li.Assignee != nil {
		issue.Assignee = li.Assignee.Name
	}
	if li.Creator != nil {
		issue.Reporter = li.Creator.Name
	}
	if len(li.Labels.Nodes) > 0 {
		issue.Type = li.Labels.Nodes[0].Name
	}

	return issue
}

// projectFromIdentifier extracts the team key from an identifier like "ENG-123".
func projectFromIdentifier(identifier string) string {
	idx := strings.LastIndex(identifier, "-")
	if idx < 0 {
		return ""
	}
	return identifier[:idx]
}
