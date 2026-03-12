package claude

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/StephanSchmidt/human/errors"
)

//go:embed embed/human-plan-skill.md
var skillContent []byte

//go:embed embed/human-planner-agent.md
var agentContent []byte

//go:embed embed/human-ready-skill.md
var readySkillContent []byte

//go:embed embed/human-ready-agent.md
var readyAgentContent []byte

//go:embed embed/human-bug-plan-skill.md
var bugPlanSkillContent []byte

//go:embed embed/human-bug-analyzer-agent.md
var bugAnalyzerAgentContent []byte

var userHomeDir = os.UserHomeDir

// FileWriter abstracts filesystem operations for testability.
type FileWriter interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	ReadFile(name string) ([]byte, error)
}

// OSFileWriter implements FileWriter using the os package.
type OSFileWriter struct{}

func (OSFileWriter) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (OSFileWriter) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (OSFileWriter) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(name))
}

type embeddedFile struct {
	content []byte
	relPath string
}

// Install writes the Claude Code skill and agent files to disk.
// When personal is true, files are written under ~/.claude/ instead of .claude/.
func Install(w io.Writer, fw FileWriter, personal bool) error {
	baseDir := ".claude"
	if personal {
		home, err := userHomeDir()
		if err != nil {
			return errors.WrapWithDetails(err, "resolving home directory")
		}
		baseDir = filepath.Join(home, ".claude")
	}

	files := []embeddedFile{
		{content: skillContent, relPath: filepath.Join("skills", "human-plan", "SKILL.md")},
		{content: agentContent, relPath: filepath.Join("agents", "human-planner.md")},
		{content: readySkillContent, relPath: filepath.Join("skills", "human-ready", "SKILL.md")},
		{content: readyAgentContent, relPath: filepath.Join("agents", "human-ready.md")},
		{content: bugPlanSkillContent, relPath: filepath.Join("skills", "human-bug-plan", "SKILL.md")},
		{content: bugAnalyzerAgentContent, relPath: filepath.Join("agents", "human-bug-analyzer.md")},
	}

	for _, f := range files {
		dest := filepath.Join(baseDir, f.relPath)

		if err := fw.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return errors.WrapWithDetails(err, "creating directory",
				"path", filepath.Dir(dest))
		}

		existing, err := fw.ReadFile(dest)
		if err == nil && string(existing) == string(f.content) {
			_, _ = fmt.Fprintf(w, "  unchanged %s\n", dest)
			continue
		}

		action := "created"
		if err == nil {
			action = "updated"
		}

		if err := fw.WriteFile(dest, f.content, 0o644); err != nil {
			return errors.WrapWithDetails(err, "writing file",
				"path", dest)
		}

		_, _ = fmt.Fprintf(w, "  %s %s\n", action, dest)
	}

	return nil
}
