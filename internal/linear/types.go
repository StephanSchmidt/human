package linear

import "encoding/json"

// graphQLRequest is the generic GraphQL request envelope.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLResponse is the generic GraphQL response envelope.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

// graphQLError represents a single error from the GraphQL API.
type graphQLError struct {
	Message string `json:"message"`
}

// linearIssue is the Linear API representation of an issue.
type linearIssue struct {
	Identifier    string          `json:"identifier"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	State         nameNode        `json:"state"`
	PriorityLabel string          `json:"priorityLabel"`
	Assignee      *nameNode       `json:"assignee"`
	Creator       *nameNode       `json:"creator"`
	Labels        labelConnection `json:"labels"`
}

type nameNode struct {
	Name string `json:"name"`
}

type labelConnection struct {
	Nodes []nameNode `json:"nodes"`
}

// Response wrappers for specific queries.

type issuesData struct {
	Issues issueConnection `json:"issues"`
}

type issueConnection struct {
	Nodes []linearIssue `json:"nodes"`
}

type issueData struct {
	Issue linearIssue `json:"issue"`
}

type teamsData struct {
	Teams teamConnection `json:"teams"`
}

type teamConnection struct {
	Nodes []teamNode `json:"nodes"`
}

type teamNode struct {
	ID string `json:"id"`
}

type issueCreateData struct {
	IssueCreate issueCreatePayload `json:"issueCreate"`
}

type issueCreatePayload struct {
	Success bool        `json:"success"`
	Issue   linearIssue `json:"issue"`
}
