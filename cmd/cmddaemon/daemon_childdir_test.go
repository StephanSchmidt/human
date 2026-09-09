package cmddaemon

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The re-exec'd foreground child must land in the project, not in whatever
// directory the launcher stood in — "/" for a desktop-launched daemon (SC-4819).
func TestDaemonChildDir(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone")

	assert.Equal(t, dir, daemonChildDir([]string{dir}))
	assert.Equal(t, dir, daemonChildDir([]string{missing, dir}), "a vanished project is skipped, not passed to exec")
	assert.Empty(t, daemonChildDir(nil), "no project inherits the launcher's directory, as before")
	assert.Empty(t, daemonChildDir([]string{missing}))
}
