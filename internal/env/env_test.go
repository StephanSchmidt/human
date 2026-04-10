package env

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithEnv_NilMap(t *testing.T) {
	ctx := context.Background()
	got := WithEnv(ctx, nil)
	assert.Equal(t, ctx, got, "nil map should return the same context")
}

func TestWithEnv_NonNilMap(t *testing.T) {
	ctx := context.Background()
	m := map[string]string{"KEY": "val"}
	got := WithEnv(ctx, m)
	assert.NotEqual(t, ctx, got, "non-nil map should return a new context")
}

func TestLookup_FromContextMap(t *testing.T) {
	ctx := WithEnv(context.Background(), map[string]string{"MY_KEY": "from_ctx"})
	assert.Equal(t, "from_ctx", Lookup(ctx, "MY_KEY"))
}

func TestLookup_FallsBackToOsGetenv(t *testing.T) {
	t.Setenv("TEST_ENV_FALLBACK", "from_os")
	ctx := WithEnv(context.Background(), map[string]string{})
	assert.Equal(t, "from_os", Lookup(ctx, "TEST_ENV_FALLBACK"))
}

func TestLookup_NilContext(t *testing.T) {
	t.Setenv("TEST_ENV_NIL_CTX", "fallback")
	//nolint:staticcheck // intentionally passing nil context to test fallback
	assert.Equal(t, "fallback", Lookup(nil, "TEST_ENV_NIL_CTX"))
}

func TestLookup_NoEnvOnContext(t *testing.T) {
	t.Setenv("TEST_ENV_NO_MAP", "process_env")
	ctx := context.Background()
	assert.Equal(t, "process_env", Lookup(ctx, "TEST_ENV_NO_MAP"))
}

func TestLookup_ContextOverridesOsEnv(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "os_value")
	ctx := WithEnv(context.Background(), map[string]string{"OVERRIDE_KEY": "ctx_value"})
	assert.Equal(t, "ctx_value", Lookup(ctx, "OVERRIDE_KEY"))
}
