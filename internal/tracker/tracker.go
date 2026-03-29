package tracker

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/StephanSchmidt/human/errors"
)

// githubIssueRe matches GitHub issue keys like "owner/repo#123".
var githubIssueRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+#\d+$`)

// githubRepoRe matches GitHub project keys like "owner/repo".
var githubRepoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// jiraLinearIssueRe matches Jira/Linear issue keys like "KAN-42" or "ENG-123".
var jiraLinearIssueRe = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)

// numericRe matches purely numeric keys like "123" (Shortcut).
var numericRe = regexp.MustCompile(`^\d+$`)

// azureDevOpsRe matches Azure DevOps work item keys like "Project/42".
var azureDevOpsRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*/\d+$`)

// DetectCandidateKinds returns all tracker kinds whose key format matches the
// given key. The order is deterministic: azuredevops is checked before
// github/gitlab repo format since "Word/N" is a subset of "owner/repo".
func DetectCandidateKinds(key string) []string {
	if key == "" {
		return nil
	}

	var kinds []string

	if jiraLinearIssueRe.MatchString(key) {
		kinds = append(kinds, "jira", "linear")
	}

	if githubIssueRe.MatchString(key) {
		kinds = append(kinds, "github", "gitlab")
	}

	// Check azureDevOpsRe before githubRepoRe — "Project/42" matches both.
	if azureDevOpsRe.MatchString(key) {
		kinds = append(kinds, "azuredevops")
	} else if githubRepoRe.MatchString(key) {
		kinds = append(kinds, "github", "gitlab")
	}

	if numericRe.MatchString(key) {
		kinds = append(kinds, "shortcut")
	}

	return kinds
}

// ExtractProject extracts the project identifier from a key.
//
//	"KAN-42"              → "KAN"
//	"octocat/repo#42"     → "octocat/repo"
//	"octocat/repo"        → "octocat/repo"
//	"Project/42"          → "Project"
//	"123"                 → ""
func ExtractProject(key string) string {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx]
	}
	if jiraLinearIssueRe.MatchString(key) {
		return key[:strings.LastIndex(key, "-")]
	}
	if azureDevOpsRe.MatchString(key) {
		return key[:strings.LastIndex(key, "/")]
	}
	if githubRepoRe.MatchString(key) {
		return key
	}
	return ""
}

// FindResult holds the outcome of FindTracker.
type FindResult struct {
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Key      string `json:"key"`
}

// FindTracker determines which configured tracker owns the given key.
//
// Resolution strategy:
//  1. Match key format against regexes → candidate kinds
//  2. Filter candidates against configured instances
//  3. If one kind remains → return it (no API call)
//  4. If ambiguous → probe each instance with GetIssue until one succeeds
func FindTracker(ctx context.Context, key string, instances []Instance) (*FindResult, error) {
	candidates := DetectCandidateKinds(key)
	if len(candidates) == 0 {
		return nil, errors.WithDetails("unrecognized key format", "key", key)
	}

	// Filter to kinds that are actually configured.
	candidateSet := make(map[string]bool, len(candidates))
	for _, k := range candidates {
		candidateSet[k] = true
	}

	var matching []Instance
	seenKinds := make(map[string]bool)
	for _, inst := range instances {
		if candidateSet[inst.Kind] {
			matching = append(matching, inst)
			seenKinds[inst.Kind] = true
		}
	}

	if len(matching) == 0 {
		return nil, errors.WithDetails("no configured tracker matches key format", "key", key)
	}

	// If all matching instances are the same kind, no ambiguity.
	if len(seenKinds) == 1 {
		kind := matching[0].Kind
		return &FindResult{
			Provider: kind,
			Project:  ExtractProject(key),
			Key:      key,
		}, nil
	}

	// Ambiguous — probe each instance.
	return probeInstances(ctx, key, matching)
}

// probeTimeout is the per-provider timeout for probing instances.
const probeTimeout = 10 * time.Second

// probeInstances tries GetIssue on each instance and returns the first success.
func probeInstances(ctx context.Context, key string, instances []Instance) (*FindResult, error) {
	for _, inst := range instances {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		_, err := inst.Provider.GetIssue(probeCtx, key)
		cancel()
		if err == nil {
			return &FindResult{
				Provider: inst.Kind,
				Project:  ExtractProject(key),
				Key:      key,
			}, nil
		}
	}
	return nil, errors.WithDetails("no configured tracker recognized the key", "key", key)
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
	Key         string    `json:"key"`
	Project     string    `json:"project"`                    // project key, e.g. "KAN"
	Type        string    `json:"type"`                       // issue type, e.g. "Task", "Bug"
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	StatusType  string    `json:"status_type,omitempty"`      // "unstarted", "started", "done", "closed", or ""
	Priority    string    `json:"priority"`
	Assignee    string    `json:"assignee"`
	Reporter    string    `json:"reporter"`
	Description string    `json:"description"`                // markdown
	UpdatedAt   time.Time `json:"updated_at"`                 // last modification timestamp
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
	Project      string
	MaxResults   int
	IncludeAll   bool      // when false, only open/active issues are returned
	UpdatedSince time.Time // when non-zero, only return issues updated after this time
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
	Transitioner
	Assigner
	CurrentUserGetter
	Editor
	StatusLister
}

// Instance represents a configured tracker backend ready for use.
type Instance struct {
	Name        string   // config entry name ("work", "personal"), empty for CLI-flag instances
	Kind        string   // "jira", "github", "linear"
	URL         string   // display URL
	User        string   // display user (Jira only)
	Description string   // optional human-readable description of what this tracker is for
	Safe        bool     // when true, destructive operations (deletes) are blocked
	Projects    []string // projects to index (e.g. ["KAN", "INFRA"])
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

// Assigner assigns an issue to a user.
type Assigner interface {
	AssignIssue(ctx context.Context, key string, userID string) error
}

// CurrentUserGetter retrieves the authenticated user's identifier.
type CurrentUserGetter interface {
	GetCurrentUser(ctx context.Context) (string, error)
}

// EditOptions specifies which fields to update on an issue.
// Nil pointer fields are left unchanged; non-nil fields are set (even if empty).
type EditOptions struct {
	Title       *string
	Description *string
}

// Editor updates an existing issue's title and/or description.
type Editor interface {
	EditIssue(ctx context.Context, key string, opts EditOptions) (*Issue, error)
}

// Status represents a workflow state that an issue can be in.
type Status struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"` // "unstarted", "started", "done", "closed", or ""
}

// StatusLister lists available statuses for an issue.
// For Jira, only valid transitions from the current state are returned.
// For other trackers, all statuses for the project/workflow are returned.
type StatusLister interface {
	ListStatuses(ctx context.Context, key string) ([]Status, error)
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

// ResolveByKind returns the first instance matching the given tracker kind.
// When name is non-empty, it further filters to that named instance.
func ResolveByKind(kind string, instances []Instance, name string) (*Instance, error) {
	var filtered []Instance
	for _, inst := range instances {
		if inst.Kind == kind {
			filtered = append(filtered, inst)
		}
	}
	if len(filtered) == 0 {
		return nil, errors.WithDetails("no tracker of kind configured", "kind", kind)
	}
	if name != "" {
		for i := range filtered {
			if filtered[i].Name == name {
				return &filtered[i], nil
			}
		}
		return nil, errors.WithDetails("tracker name not found for kind", "name", name, "kind", kind)
	}
	return &filtered[0], nil
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
