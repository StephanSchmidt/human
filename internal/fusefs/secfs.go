package fusefs

import (
	"context"
	"path/filepath"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// SecNode is a FUSE inode that mirrors a real file or directory.
// For non-.env files, it delegates to LoopbackNode (passthrough).
// For .env files, it serves empty content and blocks writes.
type SecNode struct {
	*fs.LoopbackNode
}

var _ = (fs.NodeWrapChilder)((*SecNode)(nil))
var _ = (fs.NodeOpener)((*SecNode)(nil))
var _ = (fs.NodeGetattrer)((*SecNode)(nil))

// WrapChild ensures every child inode created by Lookup/Create is a SecNode.
func (n *SecNode) WrapChild(_ context.Context, ops fs.InodeEmbedder) fs.InodeEmbedder {
	lb, ok := ops.(*fs.LoopbackNode)
	if !ok {
		return ops
	}
	return &SecNode{LoopbackNode: lb}
}

func (n *SecNode) isEnv() bool {
	return IsEnvFile(filepath.Base(n.Path(n.root())))
}

func (n *SecNode) root() *fs.Inode {
	if n.RootData.RootNode != nil {
		return n.RootData.RootNode.EmbeddedInode()
	}
	return n.Root()
}

// Open intercepts .env files and returns an empty read-only handle.
// All other files delegate to LoopbackNode.Open for passthrough I/O.
func (n *SecNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if n.isEnv() {
		return &emptyFileHandle{}, fuse.FOPEN_DIRECT_IO, fs.OK
	}
	return n.LoopbackNode.Open(ctx, flags)
}

// Getattr for .env files reports size 0.
func (n *SecNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	errno := n.LoopbackNode.Getattr(ctx, f, out)
	if errno != fs.OK {
		return errno
	}
	if n.isEnv() {
		out.Size = 0
	}
	return fs.OK
}

// emptyFileHandle serves empty content and rejects writes.
type emptyFileHandle struct{}

var _ = (fs.FileReader)((*emptyFileHandle)(nil))
var _ = (fs.FileWriter)((*emptyFileHandle)(nil))
var _ = (fs.FileGetattrer)((*emptyFileHandle)(nil))

func (h *emptyFileHandle) Read(_ context.Context, _ []byte, _ int64) (fuse.ReadResult, syscall.Errno) {
	return fuse.ReadResultData(nil), fs.OK
}

func (h *emptyFileHandle) Write(_ context.Context, _ []byte, _ int64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS
}

func (h *emptyFileHandle) Getattr(_ context.Context, out *fuse.AttrOut) syscall.Errno {
	out.Size = 0
	return fs.OK
}

// NewSecRoot creates a new SecNode-based loopback root for the given directory.
func NewSecRoot(rootPath string) (fs.InodeEmbedder, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(rootPath, &st); err != nil {
		return nil, err
	}

	root := &fs.LoopbackRoot{
		Path: rootPath,
		Dev:  uint64(st.Dev),
	}

	lb := &fs.LoopbackNode{RootData: root}
	sec := &SecNode{LoopbackNode: lb}
	root.RootNode = sec
	return sec, nil
}
