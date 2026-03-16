package tracker

import (
	"context"

	"github.com/StephanSchmidt/human/errors"
)

// SafeProvider wraps a Provider and blocks destructive operations.
// Only DeleteIssue is blocked; all other methods delegate to the inner provider.
type SafeProvider struct {
	inner        Provider
	instanceName string
}

// NewSafeProvider creates a SafeProvider that delegates to inner and blocks
// DeleteIssue with a descriptive error.
func NewSafeProvider(inner Provider, instanceName string) *SafeProvider {
	return &SafeProvider{inner: inner, instanceName: instanceName}
}

func (s *SafeProvider) ListIssues(ctx context.Context, opts ListOptions) ([]Issue, error) {
	return s.inner.ListIssues(ctx, opts)
}

func (s *SafeProvider) GetIssue(ctx context.Context, key string) (*Issue, error) {
	return s.inner.GetIssue(ctx, key)
}

func (s *SafeProvider) CreateIssue(ctx context.Context, issue *Issue) (*Issue, error) {
	return s.inner.CreateIssue(ctx, issue)
}

func (s *SafeProvider) DeleteIssue(_ context.Context, _ string) error {
	return errors.WithDetails("operation blocked by safe mode: %s on %s",
		"operation", "DeleteIssue",
		"instance", s.instanceName)
}

func (s *SafeProvider) ListComments(ctx context.Context, issueKey string) ([]Comment, error) {
	return s.inner.ListComments(ctx, issueKey)
}

func (s *SafeProvider) AddComment(ctx context.Context, issueKey string, body string) (*Comment, error) {
	return s.inner.AddComment(ctx, issueKey, body)
}

func (s *SafeProvider) TransitionIssue(ctx context.Context, key string, targetStatus string) error {
	return s.inner.TransitionIssue(ctx, key, targetStatus)
}

func (s *SafeProvider) AssignIssue(ctx context.Context, key string, userID string) error {
	return s.inner.AssignIssue(ctx, key, userID)
}

func (s *SafeProvider) GetCurrentUser(ctx context.Context) (string, error) {
	return s.inner.GetCurrentUser(ctx)
}

func (s *SafeProvider) EditIssue(ctx context.Context, key string, opts EditOptions) (*Issue, error) {
	return s.inner.EditIssue(ctx, key, opts)
}

func (s *SafeProvider) ListStatuses(ctx context.Context, key string) ([]Status, error) {
	return s.inner.ListStatuses(ctx, key)
}
