package devcontainer

import (
	"strings"

	"github.com/rs/zerolog"
)

// ParseRunArgs translates devcontainer.json runArgs (Docker CLI flags) into
// ContainerCreateOptions fields. Unknown flags are logged as warnings.
func ParseRunArgs(args []string, opts *ContainerCreateOptions, logger zerolog.Logger) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, hasEq := strings.Cut(arg, "=")

		// Consume the next arg as value for space-separated form.
		if !hasEq && needsValue(key) && i+1 < len(args) {
			i++
			val = args[i]
		}

		applyRunArg(key, val, opts, logger, arg)
	}
}

// needsValue returns true if the flag takes a value argument.
func needsValue(key string) bool {
	switch key {
	case "--add-host", "--cap-add", "--security-opt", "--network":
		return true
	}
	return false
}

// applyRunArg applies a single runArg flag to the create options.
func applyRunArg(key, val string, opts *ContainerCreateOptions, logger zerolog.Logger, raw string) {
	switch key {
	case "--add-host":
		opts.ExtraHosts = append(opts.ExtraHosts, val)
	case "--cap-add":
		opts.CapAdd = append(opts.CapAdd, val)
	case "--security-opt":
		opts.SecurityOpt = append(opts.SecurityOpt, val)
	case "--privileged":
		opts.Privileged = true
	case "--network":
		opts.NetworkMode = val
	default:
		logger.Warn().Str("flag", raw).Msg("unsupported runArg, skipping")
	}
}
