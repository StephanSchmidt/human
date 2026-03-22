package apiclient

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardURL(t *testing.T) {
	base, _ := url.Parse("https://example.com")
	builder := StandardURL()

	result, err := builder(base, "/api/v1/items", "page=2&limit=10")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api/v1/items?page=2&limit=10", result)
}

func TestStandardURL_noQuery(t *testing.T) {
	base, _ := url.Parse("https://example.com")
	builder := StandardURL()

	result, err := builder(base, "/api/v1/items", "")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api/v1/items", result)
}

func TestRawPathURL(t *testing.T) {
	base, _ := url.Parse("https://gitlab.com")
	builder := RawPathURL()

	result, err := builder(base, "/api/v4/projects/mygroup%2Fmyproject/issues", "per_page=20")
	require.NoError(t, err)
	assert.Contains(t, result, "mygroup%2Fmyproject")
	assert.Contains(t, result, "per_page=20")
}

func TestParsePathURL(t *testing.T) {
	base, _ := url.Parse("https://api.notion.com")
	builder := ParsePathURL()

	result, err := builder(base, "/v1/blocks/abc/children?page_size=100", "")
	require.NoError(t, err)
	assert.Equal(t, "https://api.notion.com/v1/blocks/abc/children?page_size=100", result)
}

func TestParsePathURL_withRawQuery(t *testing.T) {
	base, _ := url.Parse("https://api.example.com")
	builder := ParsePathURL()

	result, err := builder(base, "/path", "explicit=true")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/path?explicit=true", result)
}
