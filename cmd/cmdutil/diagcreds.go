package cmdutil

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// sectionToKind maps .humanconfig YAML section names to tracker kinds.
var sectionToKind = map[string]string{
	"jiras":       "jira",
	"githubs":     "github",
	"gitlabs":     "gitlab",
	"linears":     "linear",
	"azuredevops": "azuredevops",
	"shortcuts":   "shortcut",
}

// configuredEntry reads all fields we need to diagnose missing credentials.
type configuredEntry struct {
	Name   string `mapstructure:"name"`
	Key    string `mapstructure:"key"`
	User   string `mapstructure:"user"`
	Token  string `mapstructure:"token"`
	Secret string `mapstructure:"secret"` // #nosec G117 -- config field name, not an actual secret value
}

// fieldValue returns the config value for a credential suffix.
func (e configuredEntry) fieldValue(suffix string) string {
	switch suffix {
	case "KEY":
		return e.Key
	case "USER":
		return e.User
	case "TOKEN":
		return e.Token
	case "SECRET":
		return e.Secret
	default:
		return ""
	}
}

// WarnSkippedTrackers checks which trackers are configured in .humanconfig but
// did not produce loaded instances (typically due to missing credentials) and
// writes diagnostic messages to w.
// Returns true if any trackers were skipped.
func WarnSkippedTrackers(w io.Writer, dir string, loaded []tracker.Instance) bool {
	loadedSet := make(map[string]map[string]bool) // kind → set of names
	for _, inst := range loaded {
		if loadedSet[inst.Kind] == nil {
			loadedSet[inst.Kind] = make(map[string]bool)
		}
		loadedSet[inst.Kind][inst.Name] = true
	}

	anySkipped := false
	for section, kind := range sectionToKind {
		var entries []configuredEntry
		_ = config.UnmarshalSection(dir, section, &entries)

		for _, entry := range entries {
			if loadedSet[kind][entry.Name] {
				continue
			}

			spec, ok := tracker.CredSpecForKind(kind)
			if !ok {
				continue
			}

			// Check which creds are truly missing: not in config, not in
			// per-instance env (PREFIX_NAME_SUFFIX), not in global env (PREFIX_SUFFIX).
			missing := findMissing(spec, entry)
			if len(missing) == 0 {
				// All creds seem present but instance still didn't load — generic message.
				anySkipped = true
				_, _ = fmt.Fprintf(w, "Skipped %s/%s: credentials incomplete\n", kind, entry.Name)
				continue
			}

			anySkipped = true
			_, _ = fmt.Fprintf(w, "Skipped %s/%s: missing %s\n", kind, entry.Name, strings.Join(missing, ", "))
		}
	}

	return anySkipped
}

// findMissing returns env var names for credentials that are not provided by
// the config file, per-instance env vars, or global env vars.
func findMissing(spec tracker.CredSpec, entry configuredEntry) []string {
	var missing []string
	for _, suffix := range spec.Required {
		// 1. Check config file field.
		if entry.fieldValue(suffix) != "" {
			continue
		}
		// 2. Check per-instance env var: PREFIX_NAME_SUFFIX.
		if entry.Name != "" {
			instEnv := spec.EnvPrefix + "_" + strings.ToUpper(entry.Name) + "_" + suffix
			if os.Getenv(instEnv) != "" {
				continue
			}
		}
		// 3. Check global env var: PREFIX_SUFFIX.
		globalEnv := spec.EnvPrefix + "_" + suffix
		if os.Getenv(globalEnv) != "" {
			continue
		}
		missing = append(missing, spec.EnvPrefix+"_"+suffix)
	}
	return missing
}
