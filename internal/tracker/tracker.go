package tracker

import (
	"context"
	"time"

	"github.com/stephanschmidt/human/errors"
)

// Issue is a provider-agnostic issue representation.
type Issue struct {
	Key         string `json:"key"`
	Project     string `json:"project"`     // project key, e.g. "KAN"
	Type        string `json:"type"`        // issue type, e.g. "Task", "Bug"
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
}

// Instance represents a configured tracker backend ready for use.
type Instance struct {
	Name     string   // config entry name ("work", "personal"), empty for CLI-flag instances
	Kind     string   // "jira", "github", "linear"
	URL      string   // display URL
	User     string   // display user (Jira only)
	Provider Provider
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

// Transitioner moves an issue to a new status.
type Transitioner interface {
	TransitionIssue(ctx context.Context, key string, targetStatus string) error
}

// Resolve determines which tracker instance to use.
//
// When name is provided it finds the single instance whose Name matches.
// When name is empty it auto-detects: if all instances share one Kind it
// returns the first; if multiple kinds exist it returns an error.
func Resolve(name string, instances []Instance) (*Instance, error) {
	if name != "" {
		return resolveByName(name, instances)
	}
	return resolveAutoDetect(instances)
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

// resolveAutoDetect picks the sole kind of configured instances. If multiple
// kinds exist an error is returned asking the user to specify --tracker.
func resolveAutoDetect(instances []Instance) (*Instance, error) {
	if len(instances) == 0 {
		return nil, errors.WithDetails("no tracker configured, add jiras:, githubs:, or linears: to .humanconfig.yaml")
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
