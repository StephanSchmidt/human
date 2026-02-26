package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/tracker"
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

const getIssueIDQuery = `query($id: String!) {
	issue(id: $id) { id }
}`

const listCommentsQuery = `query($id: String!) {
	issue(id: $id) {
		comments { nodes { id body createdAt user { name } } }
	}
}`

const addCommentMutation = `mutation($issueId: String!, $body: String!) {
	commentCreate(input: { issueId: $issueId, body: $body }) {
		success
		comment { id body createdAt user { name } }
	}
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
	http    tracker.HTTPDoer
}

// New creates a Linear client with the given base URL and API key.
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: http.DefaultClient}
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

// AddComment implements tracker.Commenter.
func (c *Client) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	issueID, err := c.resolveIssueID(ctx, issueKey)
	if err != nil {
		return nil, err
	}

	vars := map[string]any{
		"issueId": issueID,
		"body":    body,
	}

	data, err := c.doGraphQL(ctx, addCommentMutation, vars)
	if err != nil {
		return nil, err
	}

	var result commentCreateData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comment create response",
			"issueKey", issueKey)
	}

	if !result.CommentCreate.Success {
		return nil, errors.WithDetails("linear comment creation failed",
			"issueKey", issueKey)
	}

	return toTrackerComment(result.CommentCreate.Comment)
}

// ListComments implements tracker.Commenter.
func (c *Client) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	vars := map[string]any{"id": issueKey}

	data, err := c.doGraphQL(ctx, listCommentsQuery, vars)
	if err != nil {
		return nil, err
	}

	var result issueCommentsData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comments response",
			"issueKey", issueKey)
	}

	comments := make([]tracker.Comment, 0, len(result.Issue.Comments.Nodes))
	for _, lc := range result.Issue.Comments.Nodes {
		c, err := toTrackerComment(lc)
		if err != nil {
			return nil, err
		}
		comments = append(comments, *c)
	}
	return comments, nil
}

// resolveIssueID looks up the internal Linear issue ID for an identifier.
func (c *Client) resolveIssueID(ctx context.Context, identifier string) (string, error) {
	vars := map[string]any{"id": identifier}

	data, err := c.doGraphQL(ctx, getIssueIDQuery, vars)
	if err != nil {
		return "", err
	}

	var result issueIDData
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.WrapWithDetails(err, "decoding issue ID response",
			"identifier", identifier)
	}

	if result.Issue.ID == "" {
		return "", errors.WithDetails("linear issue not found",
			"identifier", identifier)
	}

	return result.Issue.ID, nil
}

func toTrackerComment(lc linearComment) (*tracker.Comment, error) {
	created, err := time.Parse(time.RFC3339, lc.CreatedAt)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing comment timestamp",
			"commentID", lc.ID)
	}

	author := ""
	if lc.User != nil {
		author = lc.User.Name
	}

	return &tracker.Comment{
		ID:      lc.ID,
		Author:  author,
		Body:    lc.Body,
		Created: created,
	}, nil
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
	if err := tracker.ValidateURL(endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"endpoint", endpoint)
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
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
