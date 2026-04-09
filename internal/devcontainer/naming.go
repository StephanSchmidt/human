package devcontainer

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Docker labels for identifying human-managed devcontainers.
const (
	LabelManaged    = "dev.human.managed"     // "true"
	LabelProject    = "dev.human.project"     // absolute path to project dir
	LabelConfigHash = "dev.human.config-hash" // SHA256 of devcontainer.json
	LabelName       = "dev.human.name"        // human-friendly name
)

var unsafeChars = regexp.MustCompile(`[^a-z0-9-]`)

// SanitizeName converts a project directory basename into a Docker-safe name.
// Lowercased, non-alphanumeric chars replaced with hyphens, trimmed.
func SanitizeName(name string) string {
	s := strings.ToLower(name)
	s = unsafeChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "devcontainer"
	}
	return s
}

// ImageName returns the Docker image tag for a devcontainer build.
// Format: human-dc-<project>:<hash12>
func ImageName(projectDir, configHash string) string {
	name := SanitizeName(filepath.Base(projectDir))
	prefix := configHash
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return fmt.Sprintf("human-dc-%s:%s", name, prefix)
}

// ContainerName returns the Docker container name for a devcontainer.
// Format: human-dc-<project>
func ContainerName(projectDir string) string {
	name := SanitizeName(filepath.Base(projectDir))
	return fmt.Sprintf("human-dc-%s", name)
}

// ConfigHash returns a hex-encoded SHA256 hash of devcontainer.json content.
func ConfigHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// ManagedLabels returns the standard set of labels for a human-managed container.
func ManagedLabels(projectDir, name, configHash string) map[string]string {
	return map[string]string{
		LabelManaged:    "true",
		LabelProject:    projectDir,
		LabelName:       name,
		LabelConfigHash: configHash,
	}
}
