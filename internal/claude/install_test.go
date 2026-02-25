package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"human/errors"
)

type mockFileWriter struct {
	files   map[string][]byte
	dirs    map[string]bool
	mkdirFn func(path string) error
	writeFn func(name string) error
}

func newMockFileWriter() *mockFileWriter {
	return &mockFileWriter{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (m *mockFileWriter) MkdirAll(path string, _ os.FileMode) error {
	if m.mkdirFn != nil {
		if err := m.mkdirFn(path); err != nil {
			return err
		}
	}
	m.dirs[path] = true
	return nil
}

func (m *mockFileWriter) WriteFile(name string, data []byte, _ os.FileMode) error {
	if m.writeFn != nil {
		if err := m.writeFn(name); err != nil {
			return err
		}
	}
	m.files[name] = data
	return nil
}

func (m *mockFileWriter) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func TestInstall_CreatesNewFiles(t *testing.T) {
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := Install(&buf, fw, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "created")

	skillPath := filepath.Join(".claude", "skills", "human-plan", "SKILL.md")
	agentPath := filepath.Join(".claude", "agents", "human-planner.md")
	readySkillPath := filepath.Join(".claude", "skills", "human-ready", "SKILL.md")
	readyAgentPath := filepath.Join(".claude", "agents", "human-ready.md")

	assert.Equal(t, string(skillContent), string(fw.files[skillPath]))
	assert.Equal(t, string(agentContent), string(fw.files[agentPath]))
	assert.Equal(t, string(readySkillContent), string(fw.files[readySkillPath]))
	assert.Equal(t, string(readyAgentContent), string(fw.files[readyAgentPath]))
}

func TestInstall_SkipsUnchangedFiles(t *testing.T) {
	fw := newMockFileWriter()

	skillPath := filepath.Join(".claude", "skills", "human-plan", "SKILL.md")
	agentPath := filepath.Join(".claude", "agents", "human-planner.md")
	readySkillPath := filepath.Join(".claude", "skills", "human-ready", "SKILL.md")
	readyAgentPath := filepath.Join(".claude", "agents", "human-ready.md")
	fw.files[skillPath] = skillContent
	fw.files[agentPath] = agentContent
	fw.files[readySkillPath] = readySkillContent
	fw.files[readyAgentPath] = readyAgentContent

	var buf bytes.Buffer
	err := Install(&buf, fw, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "unchanged")
	assert.NotContains(t, buf.String(), "created")
	assert.NotContains(t, buf.String(), "updated")
}

func TestInstall_OverwritesChangedFiles(t *testing.T) {
	fw := newMockFileWriter()

	skillPath := filepath.Join(".claude", "skills", "human-plan", "SKILL.md")
	fw.files[skillPath] = []byte("old content")

	var buf bytes.Buffer
	err := Install(&buf, fw, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "updated "+skillPath)
	assert.Equal(t, string(skillContent), string(fw.files[skillPath]))
}

func TestInstall_CreatesParentDirectories(t *testing.T) {
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := Install(&buf, fw, false)

	require.NoError(t, err)

	skillDir := filepath.Join(".claude", "skills", "human-plan")
	readySkillDir := filepath.Join(".claude", "skills", "human-ready")
	agentDir := filepath.Join(".claude", "agents")
	assert.True(t, fw.dirs[skillDir], "expected plan skill parent directory to be created")
	assert.True(t, fw.dirs[readySkillDir], "expected ready skill parent directory to be created")
	assert.True(t, fw.dirs[agentDir], "expected agent parent directory to be created")
}

func TestInstall_WrapsMkdirError(t *testing.T) {
	fw := newMockFileWriter()
	fw.mkdirFn = func(_ string) error {
		return fmt.Errorf("permission denied")
	}

	var buf bytes.Buffer
	err := Install(&buf, fw, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating directory")

	details := errors.AllDetails(err)
	assert.NotEmpty(t, details["path"])
}

func TestInstall_WrapsWriteError(t *testing.T) {
	fw := newMockFileWriter()
	fw.writeFn = func(_ string) error {
		return fmt.Errorf("disk full")
	}

	var buf bytes.Buffer
	err := Install(&buf, fw, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing file")

	details := errors.AllDetails(err)
	assert.NotEmpty(t, details["path"])
}
