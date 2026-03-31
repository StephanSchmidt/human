package githelper

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRunner struct {
	fn func(name string, args ...string) ([]byte, error)
}

func (m *mockRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return m.fn(name, args...)
}

func TestCurrentBranch(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "git", name)
		assert.Equal(t, []string{"rev-parse", "--abbrev-ref", "HEAD"}, args)
		return []byte("feature/CU-abc123-fix\n"), nil
	}}}

	branch, err := h.CurrentBranch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "feature/CU-abc123-fix", branch)
}

func TestCurrentBranch_error(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(_ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("not a git repo")
	}}}

	_, err := h.CurrentBranch(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current branch")
}

func TestGetPRInfo_withNumber(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "gh", name)
		assert.Equal(t, []string{"pr", "view", "42", "--repo", "owner/repo", "--json", "number,title,url"}, args)
		return []byte(`{"number":42,"title":"Fix login","url":"https://github.com/owner/repo/pull/42"}`), nil
	}}}

	pr, err := h.GetPRInfo(context.Background(), 42, "owner/repo")
	require.NoError(t, err)
	assert.Equal(t, 42, pr.Number)
	assert.Equal(t, "Fix login", pr.Title)
	assert.Equal(t, "https://github.com/owner/repo/pull/42", pr.URL)
}

func TestGetPRInfo_currentBranch(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "gh", name)
		assert.Equal(t, []string{"pr", "view", "--json", "number,title,url"}, args)
		return []byte(`{"number":7,"title":"Add feature","url":"https://github.com/o/r/pull/7"}`), nil
	}}}

	pr, err := h.GetPRInfo(context.Background(), 0, "")
	require.NoError(t, err)
	assert.Equal(t, 7, pr.Number)
}

func TestGetPRInfo_error(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(_ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("no PR found")
	}}}

	_, err := h.GetPRInfo(context.Background(), 0, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PR info")
}

func TestGetCommitInfo_withSHA(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "git", name)
		assert.Equal(t, []string{"log", "-1", "--format=%H%n%s", "abc1234"}, args)
		return []byte("abc1234567890abcdef1234567890abcdef123456\nFix login bug\n"), nil
	}}}

	ci, err := h.GetCommitInfo(context.Background(), "abc1234")
	require.NoError(t, err)
	assert.Equal(t, "abc1234567890abcdef1234567890abcdef123456", ci.SHA)
	assert.Equal(t, "Fix login bug", ci.Subject)
}

func TestGetCommitInfo_defaultHEAD(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(_ string, args ...string) ([]byte, error) {
		assert.Equal(t, "HEAD", args[3])
		return []byte("deadbeef12345\nSome commit\n"), nil
	}}}

	ci, err := h.GetCommitInfo(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef12345", ci.SHA)
	assert.Equal(t, "Some commit", ci.Subject)
}

func TestGetCommitInfo_error(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(_ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("bad revision")
	}}}

	_, err := h.GetCommitInfo(context.Background(), "badref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "commit info")
}

func TestGetRepoSlug(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "gh", name)
		assert.Equal(t, []string{"repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"}, args)
		return []byte("owner/repo\n"), nil
	}}}

	slug, err := h.GetRepoSlug(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "owner/repo", slug)
}

func TestGetRepoSlug_empty(t *testing.T) {
	h := &Helper{Runner: &mockRunner{fn: func(_ string, _ ...string) ([]byte, error) {
		return []byte("\n"), nil
	}}}

	_, err := h.GetRepoSlug(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestCommitURL(t *testing.T) {
	assert.Equal(t, "https://github.com/owner/repo/commit/abc123", CommitURL("owner/repo", "abc123"))
}
