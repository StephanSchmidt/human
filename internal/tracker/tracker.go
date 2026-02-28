package tracker

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/stephanschmidt/human/errors"
)

// githubIssueRe matches GitHub issue keys like "owner/repo#123".
var githubIssueRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+#\d+$`)

// githubRepoRe matches GitHub project keys like "owner/repo".
var githubRepoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// ValidateURL checks that rawURL is a valid HTTP(S) URL.
// This guards against SSRF by rejecting non-HTTP schemes.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return errors.WrapWithDetails(err, "invalid URL", "url", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.WithDetails("URL scheme must be http or https", "url", rawURL, "scheme", u.Scheme)
	}
	if u.Host == "" {
		return errors.WithDetails("URL must have a host", "url", rawURL)
	}
	return nil
}

// HTTPDoer abstracts HTTP request execution for testability and to decouple
// from the concrete *http.Client type.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DetectKind returns the tracker kind that can be unambiguously inferred from
// the key format. Currently only "github" is detectable (owner/repo#N or
// owner/repo). Returns "" when the kind cannot be determined.
func DetectKind(key string) string {
	if key == "" {
		return ""
	}
	if githubIssueRe.MatchString(key) || githubRepoRe.MatchString(key) {
		return "github"
	}
	return ""
}

// Issue is a provider-agnostic issue representation.
type Issue struct {
	Key         string `json:"key"`
	Project     string `json:"project"` // project key, e.g. "KAN"
	Type        string `json:"type"`    // issue type, e.g. "Task", "Bug"
	Summary     string `json:"summary"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Assignee    string `json:"assignee"`
	Reporter    string `json:"reporter"`
	Description string `json:"description"` // markdown
}

// Comment is a provider-agnostic comment representation.
type Comment struct {
	ID      string
	Author  string
	Body    string // markdown
	Created time.Time
}

// ListOptions controls issue listing behaviour.
type ListOptions struct {
	Project    string
	MaxResults int
	IncludeAll bool // when false, only open/active issues are returned
}

// Read interfaces (implemented now).

// Lister lists issues for a project.
type Lister interface {
	ListIssues(ctx context.Context, opts ListOptions) ([]Issue, error)
}

// Getter retrieves a single issue by key.
type Getter interface {
	GetIssue(ctx context.Context, key string) (*Issue, error)
}

// Provider combines all tracker operations into a single interface.
type Provider interface {
	Lister
	Getter
	Creator
	Commenter
	Deleter
}

// Instance represents a configured tracker backend ready for use.
type Instance struct {
	Name        string // config entry name ("work", "personal"), empty for CLI-flag instances
	Kind        string // "jira", "github", "linear"
	URL         string // display URL
	User        string // display user (Jira only)
	Description string // optional human-readable description of what this tracker is for
	Provider    Provider
}

// Write interfaces (future — not implemented yet).

// Creator creates new issues.
type Creator interface {
	CreateIssue(ctx context.Context, issue *Issue) (*Issue, error)
}

// Commenter manages issue comments.
type Commenter interface {
	ListComments(ctx context.Context, issueKey string) ([]Comment, error)
	AddComment(ctx context.Context, issueKey string, body string) (*Comment, error)
}

// Deleter deletes (or closes) an issue by key.
type Deleter interface {
	DeleteIssue(ctx context.Context, key string) error
}

// Transitioner moves an issue to a new status.
type Transitioner interface {
	TransitionIssue(ctx context.Context, key string, targetStatus string) error
}

// Resolve determines which tracker instance to use.
//
// When name is provided it finds the single instance whose Name matches.
// When name is empty it auto-detects: if keyHint allows inferring the tracker
// kind it filters to that kind; otherwise if all instances share one Kind it
// returns the first; if multiple kinds exist it returns an error.
func Resolve(name string, instances []Instance, keyHint string) (*Instance, error) {
	if name != "" {
		return resolveByName(name, instances)
	}
	return resolveAutoDetect(instances, keyHint)
}

// resolveByName finds exactly one instance with the given name.
func resolveByName(name string, instances []Instance) (*Instance, error) {
	var matches []*Instance
	for i := range instances {
		if instances[i].Name == name {
			matches = append(matches, &instances[i])
		}
	}

	if len(matches) == 0 {
		return nil, errors.WithDetails("tracker name not found in .humanconfig", "name", name)
	}
	if len(matches) > 1 {
		return nil, errors.WithDetails("ambiguous tracker name found in multiple provider sections", "name", name)
	}
	return matches[0], nil
}

// resolveAutoDetect picks the sole kind of configured instances. When keyHint
// allows detecting a specific kind, instances are filtered to that kind first.
// If multiple kinds remain an error is returned asking the user to specify --tracker.
func resolveAutoDetect(instances []Instance, keyHint string) (*Instance, error) {
	if len(instances) == 0 {
		return nil, errors.WithDetails("no tracker configured, add jiras:, githubs:, gitlabs:, linears:, or shortcuts: to .humanconfig.yaml")
	}

	// Try to narrow by key format.
	if kind := DetectKind(keyHint); kind != "" {
		var filtered []Instance
		for _, inst := range instances {
			if inst.Kind == kind {
				filtered = append(filtered, inst)
			}
		}
		if len(filtered) == 0 {
			return nil, errors.WithDetails("no tracker of detected kind configured", "kind", kind, "key", keyHint)
		}
		return &filtered[0], nil
	}

	kinds := make(map[string]bool)
	for _, inst := range instances {
		kinds[inst.Kind] = true
	}

	if len(kinds) > 1 {
		return nil, errors.WithDetails("multiple tracker types configured, specify --tracker=<name>")
	}

	return &instances[0], nil
}
