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
// sourceDir. Files matching .env patterns are served as empty and read-only.
func Mount(sourceDir, mountPoint string, logger zerolog.Logger) (*MountHandle, error) {
	if err := os.MkdirAll(mountPoint, 0o750); err != nil {
		return nil, errors.WrapWithDetails(err, "creating FUSE mountpoint", "path", mountPoint)
	}

	root, err := NewSecRoot(sourceDir)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating FUSE root", "source", sourceDir)
	}

	server, err := fs.Mount(mountPoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			FsName:    sourceDir,
			Name:      "secfs",
			AllowOther: false,
		},
	})
	if err != nil {
		_ = os.Remove(mountPoint)
		return nil, errors.WrapWithDetails(err, "mounting FUSE filesystem", "mountpoint", mountPoint)
	}

	tier := detectTier(server)

	logger.Info().
		Str("source", sourceDir).
		Str("mount", mountPoint).
		Str("io", tier).
		Msg("FUSE .env filter mounted")

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
	h.logger.Info().Str("mount", h.mountPoint).Msg("FUSE .env filter unmounted")
	_ = os.Remove(h.mountPoint)
	return nil
}

// detectTier checks kernel capabilities and returns a description of the I/O
// strategy used for non-.env files.
func detectTier(server *fuse.Server) string {
	ks := server.KernelSettings()
	if ks != nil && ks.Flags64()&fuse.CAP_PASSTHROUGH != 0 {
		return fmt.Sprintf("passthrough (kernel %d.%d)", ks.Major, ks.Minor)
	}
	return "splice"
}

// IsEnvFile returns true if the filename matches .env patterns:
// .env, .env.local, .env.production, .env.*, etc.
func IsEnvFile(name string) bool {
	base := filepath.Base(name)
	return base == ".env" || strings.HasPrefix(base, ".env.")
}
