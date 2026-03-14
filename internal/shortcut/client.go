package shortcut

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
	"sync"
	"time"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/tracker"
)

var _ tracker.Provider = (*Client)(nil)

// Client is a Shortcut REST API client that implements tracker.Provider.
type Client struct {
	baseURL string
	token   string
	http    tracker.HTTPDoer

	statesMu       sync.Mutex
	states         map[int64]string // workflow_state_id → state name
	stateTypes     map[int64]string // workflow_state_id → type ("unstarted", "started", "done")
	defaultStateID int64            // first "unstarted" state (for creating stories)

	membersMu sync.Mutex
	members   map[string]string // member UUID → display name

	groupsMu sync.Mutex
	groups   map[string]string // group name → group UUID
}

// New creates a Shortcut client with the given base URL and API token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    http.DefaultClient,
		members: make(map[string]string),
	}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer tracker.HTTPDoer) {
	c.http = doer
}

// ListIssues implements tracker.Lister using GET /api/v3/groups/{id}/stories.
// The project name is resolved to a Shortcut group (team) UUID. Stories are
// fetched directly from the group endpoint to avoid search-index latency.
func (c *Client) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	project := opts.Project
	if project == "" {
		return nil, errors.WithDetails("project is required for Shortcut")
	}

	groupID, err := c.resolveGroupID(ctx, project)
	if err != nil {
		return nil, err
	}
	if groupID == "" {
		return nil, errors.WithDetails("group not found in Shortcut", "project", project)
	}

	path := fmt.Sprintf("/api/v3/groups/%s/stories", groupID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var stories []scStory
	if err := json.NewDecoder(resp.Body).Decode(&stories); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding group stories", "project", project)
	}

	issues := make([]tracker.Issue, 0, len(stories))
	for _, story := range stories {
		issue, err := c.toTrackerIssue(ctx, story, project)
		if err != nil {
			return nil, err
		}
		// toTrackerIssue loaded the workflow states; now we can filter.
		if !opts.IncludeAll && c.isDoneOrArchived(story) {
			continue
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// GetIssue implements tracker.Getter.
func (c *Client) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	id, err := parseStoryID(key)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v3/stories/%d", id)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var story scStory
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding story", "key", key)
	}

	issue, err := c.toTrackerIssue(ctx, story, "")
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// CreateIssue implements tracker.Creator.
func (c *Client) CreateIssue(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
	body := map[string]any{
		"name": issue.Title,
	}
	if issue.Description != "" {
		body["description"] = issue.Description
	}
	if isValidStoryType(issue.Type) {
		body["story_type"] = issue.Type
	}

	stateID, err := c.defaultWorkflowStateID(ctx)
	if err != nil {
		return nil, err
	}
	body["workflow_state_id"] = stateID

	if issue.Project != "" {
		groupID, err := c.resolveGroupID(ctx, issue.Project)
		if err != nil {
			return nil, err
		}
		if groupID != "" {
			body["group_id"] = groupID
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling create request")
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v3/stories", "", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var story scStory
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding create response")
	}

	return &tracker.Issue{
		Key:         strconv.FormatInt(story.ID, 10),
		Project:     issue.Project,
		Title:       story.Name,
		Description: story.Description,
		Type:        story.StoryType,
	}, nil
}

// ListStatuses implements tracker.StatusLister.
func (c *Client) ListStatuses(ctx context.Context, _ string) ([]tracker.Status, error) {
	c.statesMu.Lock()
	defer c.statesMu.Unlock()

	if c.states == nil {
		if err := c.fetchWorkflowsLocked(ctx); err != nil {
			return nil, err
		}
	}

	statuses := make([]tracker.Status, 0, len(c.states))
	for id, name := range c.states {
		statuses = append(statuses, tracker.Status{
			Name: name,
			Type: c.stateTypes[id],
		})
	}
	return statuses, nil
}

// resolveStateByName matches a target status name against cached workflow states.
// Returns the state ID or an error listing available state names.
func (c *Client) resolveStateByName(ctx context.Context, targetStatus string) (int64, error) {
	c.statesMu.Lock()
	defer c.statesMu.Unlock()

	if c.states == nil {
		if err := c.fetchWorkflowsLocked(ctx); err != nil {
			return 0, err
		}
	}

	// Try exact name match (case-insensitive).
	for id, name := range c.states {
		if strings.EqualFold(name, targetStatus) {
			return id, nil
		}
	}

	// Fall back to type-based match for backward compat with "issue start".
	targetLower := strings.ToLower(targetStatus)
	for id, typ := range c.stateTypes {
		if typ == targetLower {
			return id, nil
		}
	}

	names := make([]string, 0, len(c.states))
	for _, name := range c.states {
		names = append(names, name)
	}
	return 0, errors.WithDetails("workflow state not found",
		"targetStatus", targetStatus, "available", strings.Join(names, ", "))
}

// TransitionIssue implements tracker.Transitioner.
func (c *Client) TransitionIssue(ctx context.Context, key string, targetStatus string) error {
	id, err := parseStoryID(key)
	if err != nil {
		return err
	}

	stateID, err := c.resolveStateByName(ctx, targetStatus)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]int64{"workflow_state_id": stateID})
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling transition request", "key", key)
	}

	path := fmt.Sprintf("/api/v3/stories/%d", id)
	resp, err := c.doRequest(ctx, http.MethodPut, path, "", bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// AssignIssue implements tracker.Assigner.
func (c *Client) AssignIssue(ctx context.Context, key string, userID string) error {
	id, err := parseStoryID(key)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string][]string{"owner_ids": {userID}})
	if err != nil {
		return errors.WrapWithDetails(err, "marshalling assign request", "key", key)
	}

	path := fmt.Sprintf("/api/v3/stories/%d", id)
	resp, err := c.doRequest(ctx, http.MethodPut, path, "", bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// GetCurrentUser implements tracker.CurrentUserGetter.
func (c *Client) GetCurrentUser(ctx context.Context) (string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v3/member-info", "", nil, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var info scMemberInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", errors.WrapWithDetails(err, "decoding member-info response")
	}
	return info.ID, nil
}

// EditIssue implements tracker.Editor.
func (c *Client) EditIssue(ctx context.Context, key string, opts tracker.EditOptions) (*tracker.Issue, error) {
	id, err := parseStoryID(key)
	if err != nil {
		return nil, err
	}

	fields := make(map[string]string)
	if opts.Title != nil {
		fields["name"] = *opts.Title
	}
	if opts.Description != nil {
		fields["description"] = *opts.Description
	}

	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling edit request", "key", key)
	}

	path := fmt.Sprintf("/api/v3/stories/%d", id)
	resp, err := c.doRequest(ctx, http.MethodPut, path, "", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var story scStory
	if err := json.NewDecoder(resp.Body).Decode(&story); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding edit response", "key", key)
	}

	issue, err := c.toTrackerIssue(ctx, story, "")
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// DeleteIssue implements tracker.Deleter using true deletion (DELETE /api/v3/stories/{id}).
func (c *Client) DeleteIssue(ctx context.Context, key string) error {
	id, err := parseStoryID(key)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/api/v3/stories/%d", id)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, "", nil, "")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// AddComment implements tracker.Commenter.
func (c *Client) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	id, err := parseStoryID(issueKey)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"text": body})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling comment request", "issueKey", issueKey)
	}

	path := fmt.Sprintf("/api/v3/stories/%d/comments", id)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var sc scComment
	if err := json.NewDecoder(resp.Body).Decode(&sc); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comment response", "issueKey", issueKey)
	}

	return c.toTrackerComment(ctx, sc)
}

// ListComments implements tracker.Commenter.
func (c *Client) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	id, err := parseStoryID(issueKey)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v3/stories/%d/comments", id)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var comments []scComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comments response", "issueKey", issueKey)
	}

	result := make([]tracker.Comment, 0, len(comments))
	for _, sc := range comments {
		tc, err := c.toTrackerComment(ctx, sc)
		if err != nil {
			return nil, err
		}
		result = append(result, *tc)
	}
	return result, nil
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
	req.Header.Set("Shortcut-Token", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting Shortcut",
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting Shortcut: nil response",
			"method", method, "path", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, errors.WithDetails(
			fmt.Sprintf("shortcut %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody)),
			"statusCode", resp.StatusCode, "method", method, "path", path)
	}
	return resp, nil
}

// resolveStateName maps a workflow_state_id to its name, fetching and caching
// workflows on first call.
func (c *Client) resolveStateName(ctx context.Context, stateID int64) (string, error) {
	c.statesMu.Lock()
	defer c.statesMu.Unlock()

	if c.states == nil {
		if err := c.fetchWorkflowsLocked(ctx); err != nil {
			return "", err
		}
	}

	if name, ok := c.states[stateID]; ok {
		return name, nil
	}
	return fmt.Sprintf("Unknown(%d)", stateID), nil
}

// fetchWorkflowsLocked fetches all workflows and populates the states cache
// and defaultStateID. Must be called with statesMu held.
func (c *Client) fetchWorkflowsLocked(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v3/workflows", "", nil, "")
	if err != nil {
		return errors.WrapWithDetails(err, "fetching workflows")
	}
	defer func() { _ = resp.Body.Close() }()

	var workflows []scWorkflow
	if err := json.NewDecoder(resp.Body).Decode(&workflows); err != nil {
		return errors.WrapWithDetails(err, "decoding workflows")
	}

	c.states = make(map[int64]string)
	c.stateTypes = make(map[int64]string)
	for _, wf := range workflows {
		for _, st := range wf.States {
			c.states[st.ID] = st.Name
			c.stateTypes[st.ID] = st.Type
			if c.defaultStateID == 0 && st.Type == "unstarted" {
				c.defaultStateID = st.ID
			}
		}
	}
	return nil
}

// resolveMemberName resolves a member UUID to a display name, caching results.
func (c *Client) resolveMemberName(ctx context.Context, memberID string) (string, error) {
	if memberID == "" {
		return "", nil
	}

	c.membersMu.Lock()
	if name, ok := c.members[memberID]; ok {
		c.membersMu.Unlock()
		return name, nil
	}
	c.membersMu.Unlock()

	path := fmt.Sprintf("/api/v3/members/%s", memberID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil, "")
	if err != nil {
		return "", nil // non-fatal: return empty name on failure
	}
	defer func() { _ = resp.Body.Close() }()

	var member scMember
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		return "", nil
	}

	name := member.Profile.DisplayName
	if name == "" {
		name = member.Profile.Name
	}

	c.membersMu.Lock()
	c.members[memberID] = name
	c.membersMu.Unlock()

	return name, nil
}

// resolveGroupID maps a group name to its UUID, fetching and caching
// groups on first call. Returns empty string if the group is not found.
func (c *Client) resolveGroupID(ctx context.Context, name string) (string, error) {
	c.groupsMu.Lock()
	defer c.groupsMu.Unlock()

	if c.groups != nil {
		return c.groups[name], nil
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v3/groups", "", nil, "")
	if err != nil {
		return "", errors.WrapWithDetails(err, "fetching groups")
	}
	defer func() { _ = resp.Body.Close() }()

	var groups []scGroup
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return "", errors.WrapWithDetails(err, "decoding groups")
	}

	c.groups = make(map[string]string)
	for _, g := range groups {
		c.groups[g.Name] = g.ID
	}

	return c.groups[name], nil
}

// defaultWorkflowStateID returns the first "unstarted" workflow state ID,
// which is used as the default when creating stories. Workflows are fetched
// and cached on first call (shared with resolveStateName).
func (c *Client) defaultWorkflowStateID(ctx context.Context) (int64, error) {
	c.statesMu.Lock()
	defer c.statesMu.Unlock()

	if c.defaultStateID != 0 {
		return c.defaultStateID, nil
	}

	// If states cache is nil, we need to fetch workflows first.
	if c.states == nil {
		if err := c.fetchWorkflowsLocked(ctx); err != nil {
			return 0, err
		}
	}

	return c.defaultStateID, nil
}

// isDoneOrArchived returns true if the story is archived or in a "done" workflow state.
// Must be called after workflow states have been loaded.
func (c *Client) isDoneOrArchived(story scStory) bool {
	if story.Archived {
		return true
	}
	c.statesMu.Lock()
	stateType := c.stateTypes[story.WorkflowStateID]
	c.statesMu.Unlock()
	return stateType == "done"
}

// isValidStoryType returns true if t is a Shortcut-accepted story type.
func isValidStoryType(t string) bool {
	return t == "feature" || t == "bug" || t == "chore"
}

// parseStoryID parses a string story ID into an int64.
func parseStoryID(key string) (int64, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.WithDetails("invalid story ID, expected numeric key", "key", key)
	}
	return id, nil
}

// toTrackerIssue converts a Shortcut story to a tracker.Issue.
func (c *Client) toTrackerIssue(ctx context.Context, story scStory, project string) (tracker.Issue, error) {
	stateName, err := c.resolveStateName(ctx, story.WorkflowStateID)
	if err != nil {
		return tracker.Issue{}, err
	}

	assignee := ""
	if len(story.OwnerIDs) > 0 {
		assignee, _ = c.resolveMemberName(ctx, story.OwnerIDs[0])
	}

	reporter, _ := c.resolveMemberName(ctx, story.RequestedByID)

	return tracker.Issue{
		Key:         strconv.FormatInt(story.ID, 10),
		Project:     project,
		Type:        story.StoryType,
		Title:       story.Name,
		Status:      stateName,
		Assignee:    assignee,
		Reporter:    reporter,
		Description: story.Description,
	}, nil
}

// toTrackerComment converts a Shortcut comment to a tracker.Comment.
func (c *Client) toTrackerComment(ctx context.Context, sc scComment) (*tracker.Comment, error) {
	created, err := time.Parse(time.RFC3339, sc.CreatedAt)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing comment timestamp", "commentID", sc.ID)
	}

	author, _ := c.resolveMemberName(ctx, sc.AuthorID)

	return &tracker.Comment{
		ID:      strconv.FormatInt(sc.ID, 10),
		Author:  author,
		Body:    sc.Text,
		Created: created,
	}, nil
}
