package cmddaemon

import (
	"fmt"
	"strings"

	"github.com/gethuman-sh/human/errors"
	"github.com/gethuman-sh/human/internal/daemon"
	"github.com/gethuman-sh/human/internal/proxy"
)

// proxyPolicy is the egress decision this daemon runs, together with why it is
// what it is. The reason is carried, not derived at the point of printing,
// because a block-all policy is indistinguishable from a working one at the
// Decider interface and the daemon must be able to say which it has (SC-4819).
type proxyPolicy struct {
	// Decider is what the proxy server enforces per connection.
	Decider proxy.Decider
	// Config is the parsed proxy section, nil when none was found.
	Config *proxy.Config
	// Dir is the project directory the config came from, empty when none was.
	Dir string
	// BlockAllReason is non-empty exactly when Decider blocks every hostname
	// because no policy could be resolved. It names the directories searched so
	// the operator can see where the daemon looked.
	BlockAllReason string
}

// resolveProxyPolicy builds the daemon's egress policy from the REGISTERED
// PROJECT rather than the process working directory.
//
// The working directory was the bug: a daemon launched by the desktop app runs
// with cwd "/", found no .humanconfig.yaml there, and silently fell back to
// block-all — every container's model call died at the SNI stage with a TLS
// error that pointed at the CA instead (SC-4819). --project is honoured by
// every other subsystem; the proxy policy was the last thing keyed on cwd.
//
// Several registered projects resolve to no policy at all. Unioning their
// allowlists would widen project A's egress with project B's domains — a policy
// weakening nobody chose — so the daemon keeps blocking and says so instead.
func resolveProxyPolicy(reg *daemon.ProjectRegistry) (proxyPolicy, error) {
	dirs := registeredDirs(reg)
	if len(dirs) != 1 {
		return proxyPolicy{
			Decider:        proxy.BlockAllPolicy(),
			BlockAllReason: noSoleProjectReason(dirs),
		}, nil
	}

	dir := dirs[0]
	cfg, err := proxy.LoadConfig(dir)
	if err != nil {
		// A malformed config used to be discarded and become block-all, so a
		// typo in .humanconfig.yaml presented as a certificate failure inside
		// every container. It fails the start now, like an invalid mode already
		// did.
		return proxyPolicy{}, errors.WrapWithDetails(err, "failed to load proxy config", "dir", dir)
	}
	if cfg == nil {
		return proxyPolicy{
			Decider:        proxy.BlockAllPolicy(),
			Dir:            dir,
			BlockAllReason: "no proxy section in " + dir + "/.humanconfig.yaml",
		}, nil
	}

	policy, err := proxy.NewPolicy(cfg.Mode, cfg.Domains)
	if err != nil {
		return proxyPolicy{}, errors.WrapWithDetails(err, "invalid proxy policy", "dir", dir)
	}
	return proxyPolicy{Decider: policy, Config: cfg, Dir: dir}, nil
}

// registeredDirs lists the project directories the daemon was started with.
func registeredDirs(reg *daemon.ProjectRegistry) []string {
	if reg == nil {
		return nil
	}
	entries := reg.Entries()
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		dirs = append(dirs, e.Dir)
	}
	return dirs
}

// noSoleProjectReason explains a block-all caused by the registry rather than
// by the config: no project to read a policy from, or several with no way to
// choose between them.
func noSoleProjectReason(dirs []string) string {
	if len(dirs) == 0 {
		return "no project registered — start the daemon with --project <dir>"
	}
	return fmt.Sprintf("%d projects registered (%s) — a single egress policy cannot be chosen between them; run one daemon per project",
		len(dirs), strings.Join(dirs, ", "))
}

// blockAllStatus is the banner/log line for a daemon whose proxy blocks
// everything. Block-all used to be silent: nothing on the banner, in
// `human daemon status` or in the doctor report said the containers could not
// reach anything (SC-4819).
func blockAllStatus(reason string) string {
	return "proxy: blocking all egress — " + reason
}
