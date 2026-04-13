package vault

import (
	"context"
	"testing"
)

func TestWithResolver_roundTrip(t *testing.T) {
	r := NewResolver()
	ctx := WithResolver(context.Background(), r)
	got := ResolverFromContext(ctx)
	if got != r {
		t.Fatal("expected resolver from context to match injected resolver")
	}
}

func TestWithResolver_nilResolver(t *testing.T) {
	ctx := WithResolver(context.Background(), nil)
	got := ResolverFromContext(ctx)
	if got != nil {
		t.Fatal("expected nil resolver from context when nil was injected")
	}
}

func TestResolverFromContext_noResolver(t *testing.T) {
	got := ResolverFromContext(context.Background())
	if got != nil {
		t.Fatal("expected nil resolver from plain context")
	}
}
