package init

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPathLooker lets tests control which binaries are "found".
type mockPathLooker struct {
	available map[string]bool
}

func (m *mockPathLooker) LookPath(file string) (string, error) {
	if m.available[file] {
		return "/usr/bin/" + file, nil
	}
	return "", exec.ErrNotFound
}

func TestPrerequisitesStep_AllPresent(t *testing.T) {
	looker := &mockPathLooker{available: map[string]bool{
		"docker": true, "tmux": true,
	}}
	step := NewPrerequisitesStep(looker)
	var buf bytes.Buffer

	hints, err := step.Run(&buf, newMockFileWriter())

	require.NoError(t, err)
	assert.Nil(t, hints)
	assert.Contains(t, buf.String(), "All prerequisites satisfied.")
	assert.Contains(t, buf.String(), "docker")
	assert.Contains(t, buf.String(), "tmux")
}

func TestPrerequisitesStep_MissingDocker(t *testing.T) {
	looker := &mockPathLooker{available: map[string]bool{
		"docker": false, "tmux": true,
	}}
	step := NewPrerequisitesStep(looker)
	var buf bytes.Buffer

	_, err := step.Run(&buf, newMockFileWriter())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, buf.String(), "docker")
	assert.Contains(t, buf.String(), "https://docs.docker.com/get-docker/")
}

func TestPrerequisitesStep_AllMissing(t *testing.T) {
	looker := &mockPathLooker{available: map[string]bool{}}
	step := NewPrerequisitesStep(looker)
	var buf bytes.Buffer

	_, err := step.Run(&buf, newMockFileWriter())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, err.Error(), "tmux")

	output := buf.String()
	assert.Contains(t, output, "Missing prerequisites:")
	assert.Contains(t, output, "docker")
	assert.Contains(t, output, "tmux")
}

func TestPrerequisitesStep_Name(t *testing.T) {
	step := NewPrerequisitesStep(&mockPathLooker{})
	assert.Equal(t, "prerequisites", step.Name())
}

func TestPrerequisitesStep_CustomPrereqs(t *testing.T) {
	prereqs := []Prerequisite{
		{Binary: "foo", Purpose: "testing", InstallURL: "https://example.com/foo"},
	}
	looker := &mockPathLooker{available: map[string]bool{}}
	step := newPrerequisitesStepWith(looker, prereqs)
	var buf bytes.Buffer

	_, err := step.Run(&buf, newMockFileWriter())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo")
	assert.Contains(t, buf.String(), "https://example.com/foo")
}

func TestPrerequisiteRegistry_HasExpectedEntries(t *testing.T) {
	prereqs := PrerequisiteRegistry()
	assert.Len(t, prereqs, 2)

	binaries := make([]string, len(prereqs))
	for i, p := range prereqs {
		binaries[i] = p.Binary
		assert.NotEmpty(t, p.Purpose, "prerequisite %s has empty purpose", p.Binary)
		assert.NotEmpty(t, p.InstallURL, "prerequisite %s has empty install URL", p.Binary)
	}
	assert.Contains(t, binaries, "docker")
	assert.Contains(t, binaries, "tmux")
}
