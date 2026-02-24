package jira

import "encoding/json"

type searchResult struct {
	Issues []issue `json:"issues"`
}

type issue struct {
	Key    string      `json:"key"`
	Fields issueFields `json:"fields"`
}

type issueFields struct {
	Summary string      `json:"summary"`
	Status  statusField `json:"status"`
}

type statusField struct {
	Name string `json:"name"`
}

type issueDetail struct {
	Key    string            `json:"key"`
	Fields issueDetailFields `json:"fields"`
}

type issueDetailFields struct {
	Summary     string          `json:"summary"`
	Status      statusField     `json:"status"`
	Priority    *nameField      `json:"priority"`
	Assignee    *nameField      `json:"assignee"`
	Reporter    *nameField      `json:"reporter"`
	Description json.RawMessage `json:"description"`
}

type nameField struct {
	DisplayName string `json:"displayName"`
	Name        string `json:"name"`
}
