package github

// ghIssue is the GitHub API representation of an issue.
type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	User        *ghUser   `json:"user"`
	Assignee    *ghUser   `json:"assignee"`
	Labels      []ghLabel `json:"labels"`
	PullRequest *struct{} `json:"pull_request"` // non-nil means this is a PR
}

type ghUser struct {
	Login string `json:"login"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type createRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

type createResponse struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type commentRequest struct {
	Body string `json:"body"`
}

type ghComment struct {
	ID        int     `json:"id"`
	Body      string  `json:"body"`
	User      *ghUser `json:"user"`
	CreatedAt string  `json:"created_at"`
}
