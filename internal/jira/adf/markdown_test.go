package adf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_stringAttr(t *testing.T) {
	tests := []struct {
		name   string
		node   map[string]any
		key    string
		want   string
		wantOK bool
	}{
		{
			name:   "returns string attribute",
			node:   map[string]any{"attrs": map[string]any{"language": "go"}},
			key:    "language",
			want:   "go",
			wantOK: true,
		},
		{
			name:   "missing attrs",
			node:   map[string]any{},
			key:    "language",
			want:   "",
			wantOK: false,
		},
		{
			name:   "attrs is wrong type",
			node:   map[string]any{"attrs": "not-a-map"},
			key:    "language",
			want:   "",
			wantOK: false,
		},
		{
			name:   "key missing from attrs",
			node:   map[string]any{"attrs": map[string]any{}},
			key:    "language",
			want:   "",
			wantOK: false,
		},
		{
			name:   "value is not a string",
			node:   map[string]any{"attrs": map[string]any{"language": 42}},
			key:    "language",
			want:   "",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stringAttr(tt.node, tt.key)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func Test_intAttr(t *testing.T) {
	tests := []struct {
		name     string
		node     map[string]any
		key      string
		fallback int
		want     int
	}{
		{
			name:     "returns int from float64",
			node:     map[string]any{"attrs": map[string]any{"level": float64(3)}},
			key:      "level",
			fallback: 1,
			want:     3,
		},
		{
			name:     "missing attrs returns fallback",
			node:     map[string]any{},
			key:      "level",
			fallback: 1,
			want:     1,
		},
		{
			name:     "attrs is wrong type returns fallback",
			node:     map[string]any{"attrs": "bad"},
			key:      "level",
			fallback: 1,
			want:     1,
		},
		{
			name:     "key missing returns fallback",
			node:     map[string]any{"attrs": map[string]any{}},
			key:      "level",
			fallback: 5,
			want:     5,
		},
		{
			name:     "value is not float64 returns fallback",
			node:     map[string]any{"attrs": map[string]any{"level": "three"}},
			key:      "level",
			fallback: 1,
			want:     1,
		},
		{
			name:     "zero value is not fallback",
			node:     map[string]any{"attrs": map[string]any{"level": float64(0)}},
			key:      "level",
			fallback: 1,
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intAttr(tt.node, tt.key, tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToMarkdown_heading(t *testing.T) {
	node := map[string]any{
		"type":  "heading",
		"attrs": map[string]any{"level": float64(2)},
		"content": []any{
			map[string]any{"type": "text", "text": "Title"},
		},
	}
	assert.Equal(t, "## Title\n\n", ToMarkdown(node))
}

func TestToMarkdown_heading_default_level(t *testing.T) {
	node := map[string]any{
		"type": "heading",
		"content": []any{
			map[string]any{"type": "text", "text": "Title"},
		},
	}
	assert.Equal(t, "# Title\n\n", ToMarkdown(node))
}

func TestToMarkdown_codeBlock(t *testing.T) {
	node := map[string]any{
		"type":  "codeBlock",
		"attrs": map[string]any{"language": "go"},
		"content": []any{
			map[string]any{"type": "text", "text": "fmt.Println()"},
		},
	}
	assert.Equal(t, "```go\nfmt.Println()```\n\n", ToMarkdown(node))
}

func TestToMarkdown_codeBlock_no_language(t *testing.T) {
	node := map[string]any{
		"type": "codeBlock",
		"content": []any{
			map[string]any{"type": "text", "text": "code"},
		},
	}
	assert.Equal(t, "```\ncode```\n\n", ToMarkdown(node))
}

func TestToMarkdown_inlineCard(t *testing.T) {
	node := map[string]any{
		"type":  "inlineCard",
		"attrs": map[string]any{"url": "https://example.com"},
	}
	assert.Equal(t, "[https://example.com](https://example.com)", ToMarkdown(node))
}

func TestToMarkdown_inlineCard_no_url(t *testing.T) {
	node := map[string]any{
		"type": "inlineCard",
	}
	assert.Equal(t, "", ToMarkdown(node))
}

func TestToMarkdown_link_mark(t *testing.T) {
	node := map[string]any{
		"type": "text",
		"text": "click here",
		"marks": []any{
			map[string]any{
				"type":  "link",
				"attrs": map[string]any{"href": "https://example.com"},
			},
		},
	}
	assert.Equal(t, "[click here](https://example.com)", ToMarkdown(node))
}

func TestToMarkdown_link_mark_no_href(t *testing.T) {
	node := map[string]any{
		"type": "text",
		"text": "click here",
		"marks": []any{
			map[string]any{
				"type": "link",
			},
		},
	}
	assert.Equal(t, "click here", ToMarkdown(node))
}
