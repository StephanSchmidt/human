package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The proxy redirect is what makes the daemon's egress policy apply to a
// container, and the feature's OCI ref varies by org and version tag — so the
// predicate matches the stable middle of the reference (SC-4819).
func TestProxyRedirectEnabled(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"gethuman ref", `{"features":{"ghcr.io/gethuman-sh/treehouse/human:1":{"proxy":true}}}`, true},
		{"other org and tag", `{"features":{"ghcr.io/stephanschmidt/treehouse/human:2":{"proxy":true}}}`, true},
		{"option off", `{"features":{"ghcr.io/gethuman-sh/treehouse/human:1":{"proxy":false}}}`, false},
		{"option absent", `{"features":{"ghcr.io/gethuman-sh/treehouse/human:1":{}}}`, false},
		{"feature absent", `{"features":{"ghcr.io/devcontainers/features/node:1":{"version":"22"}}}`, false},
		{"no features", `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tc.json))
			require.NoError(t, err)
			assert.Equal(t, tc.want, ProxyRedirectEnabled(cfg))
		})
	}
}

func TestProxyRedirectEnabled_nilConfig(t *testing.T) {
	assert.False(t, ProxyRedirectEnabled(nil))
}
