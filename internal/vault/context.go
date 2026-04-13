package vault

import "context"

// resolverKey is a private context key type to avoid collisions with other
// packages stashing values on the same context.
type resolverKey struct{}

// WithResolver returns ctx augmented with the given vault Resolver.
// The daemon attaches its session-scoped resolver so per-request command
// execution reuses the same provider instance instead of creating a new
// one on every request.
func WithResolver(ctx context.Context, r *Resolver) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, resolverKey{}, r)
}

// ResolverFromContext returns the vault Resolver stored on ctx, or nil
// when none is present (direct CLI usage).
func ResolverFromContext(ctx context.Context) *Resolver {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(resolverKey{}).(*Resolver)
	return r
}
