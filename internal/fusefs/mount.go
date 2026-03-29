//go:build linux

package fusefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/rs/zerolog"

	"github.com/StephanSchmidt/human/errors"
)

// FileKind classifies a file for the FUSE security filter.
type FileKind int

const (
	FileKindNone   FileKind = iota // Not a sensitive file — passthrough.
	FileKindEnv                    // KEY=VALUE file (.env, .npmrc, .pypirc) — line-by-line redaction.
	FileKindJSON                   // JSON file (credentials.json, secrets.json) — JSON-aware redaction.
	FileKindYAML                   // YAML file (secrets.yaml) — YAML-aware redaction.
	FileKindOpaque                 // Binary/opaque (*.pem, *.key, *.p12, *.pfx) — always empty.
)

// MountHandle holds a running FUSE mount and allows unmounting.
type MountHandle struct {
	server     *fuse.Server
	mountPoint string
	tier       string
	logger     zerolog.Logger
}

// Tier returns a human-readable description of the I/O strategy in use.
func (h *MountHandle) Tier() string {
	return h.tier
}

// Mount creates a FUSE passthrough filesystem at mountPoint that mirrors
// sourceDir. Sensitive files are redacted or served empty depending on safeMode.
// When safeMode is true, all sensitive files return empty content (maximum paranoia).
// When safeMode is false, sensitive files are redacted with structure preserved.
func Mount(sourceDir, mountPoint string, safeMode bool, logger zerolog.Logger) (*MountHandle, error) {
	if err := os.MkdirAll(mountPoint, 0o750); err != nil {
		return nil, errors.WrapWithDetails(err, "creating FUSE mountpoint", "path", mountPoint)
	}

	root, err := NewSecRoot(sourceDir, safeMode)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating FUSE root", "source", sourceDir)
	}

	server, err := fs.Mount(mountPoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			FsName:     sourceDir,
			Name:       "secfs",
			AllowOther: false,
		},
	})
	if err != nil {
		_ = os.Remove(mountPoint)
		return nil, errors.WrapWithDetails(err, "mounting FUSE filesystem", "mountpoint", mountPoint)
	}

	tier := detectTier(server)

	mode := "redact"
	if safeMode {
		mode = "safe (empty)"
	}

	logger.Info().
		Str("source", sourceDir).
		Str("mount", mountPoint).
		Str("io", tier).
		Str("mode", mode).
		Msg("FUSE secret filter mounted")

	return &MountHandle{
		server:     server,
		mountPoint: mountPoint,
		tier:       tier,
		logger:     logger,
	}, nil
}

// Unmount stops the FUSE server and removes the mountpoint directory.
func (h *MountHandle) Unmount() error {
	if err := h.server.Unmount(); err != nil {
		return errors.WrapWithDetails(err, "unmounting FUSE", "mountpoint", h.mountPoint)
	}
	h.logger.Info().Str("mount", h.mountPoint).Msg("FUSE secret filter unmounted")
	_ = os.Remove(h.mountPoint)
	return nil
}

// detectTier checks kernel capabilities and returns a description of the I/O
// strategy used for non-sensitive files.
func detectTier(server *fuse.Server) string {
	ks := server.KernelSettings()
	if ks != nil && ks.Flags64()&fuse.CAP_PASSTHROUGH != 0 {
		return fmt.Sprintf("passthrough (kernel %d.%d)", ks.Major, ks.Minor)
	}
	return "splice"
}

// IsSensitiveFile classifies a filename by its sensitivity.
func IsSensitiveFile(name string) FileKind {
	base := filepath.Base(name)
	lower := strings.ToLower(base)

	// .env, .env.*
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return FileKindEnv
	}

	// .npmrc, .pypirc — KEY=VALUE style
	if lower == ".npmrc" || lower == ".pypirc" {
		return FileKindEnv
	}

	// JSON files with secrets
	if lower == "credentials.json" || lower == "service-account.json" || lower == "secrets.json" {
		return FileKindJSON
	}

	// YAML files with secrets
	if lower == "secrets.yml" || lower == "secrets.yaml" {
		return FileKindYAML
	}

	// Opaque binary/key files — always empty
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pem" || ext == ".key" || ext == ".p12" || ext == ".pfx" {
		return FileKindOpaque
	}

	return FileKindNone
}
