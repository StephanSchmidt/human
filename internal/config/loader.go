package config

import (
	"os"
	"strings"
)

// EnvField maps a config struct field to its environment variable suffix.
// For example, {Suffix: "TOKEN", Set: func(c *MyConfig, v string) { c.Token = v }}
// will check for PROVIDER_TOKEN (global) and PROVIDER_NAME_TOKEN (per-instance).
type EnvField[C any] struct {
	Suffix string
	Set    func(c *C, v string)
}

// ApplyEnvOverrides applies environment variable overrides to a config struct.
// It checks per-instance variables first (PREFIX_NAME_SUFFIX), then global
// variables (PREFIX_SUFFIX). Global overrides take precedence.
func ApplyEnvOverrides[C any](cfg *C, name, envPrefix string, fields []EnvField[C]) {
	// Per-instance overrides: PREFIX_NAME_SUFFIX
	if name != "" {
		instancePrefix := envPrefix + strings.ToUpper(name) + "_"
		for _, f := range fields {
			if v, ok := os.LookupEnv(instancePrefix + f.Suffix); ok {
				f.Set(cfg, v)
			}
		}
	}

	// Global overrides: PREFIX_SUFFIX (takes precedence)
	for _, f := range fields {
		if v, ok := os.LookupEnv(envPrefix + f.Suffix); ok {
			f.Set(cfg, v)
		}
	}
}

// InstanceSpec defines how to load and build instances from config entries.
type InstanceSpec[C any, I any] struct {
	// Section is the YAML key in .humanconfig (e.g. "githubs", "jiras").
	Section string

	// EnvPrefix is the prefix for environment variables (e.g. "GITHUB_", "JIRA_").
	EnvPrefix string

	// EnvFields maps config struct fields to env var suffixes.
	EnvFields []EnvField[C]

	// DefaultURL is set on configs with an empty URL before env overrides.
	// Leave empty if no default (e.g. Jira requires explicit URL).
	DefaultURL string

	// GetName returns the instance name from a config entry.
	GetName func(C) string

	// SetURL sets the URL on the config. Nil if the config has no URL field.
	SetURL func(*C, string)

	// GetURL returns the URL from a config entry. Nil if the config has no URL field.
	GetURL func(C) string

	// Build creates an instance from a config entry. Return (zero, false) to skip
	// the entry (e.g. missing required credentials).
	Build func(C) (I, bool)
}

// LoadInstances reads configs from a .humanconfig file, applies env overrides,
// and builds instances using the provided spec.
func LoadInstances[C any, I any](dir string, spec InstanceSpec[C, I]) ([]I, error) {
	var configs []C
	if err := UnmarshalSection(dir, spec.Section, &configs); err != nil {
		return nil, err
	}

	instances := make([]I, 0, len(configs))
	for _, cfg := range configs {
		// Apply default URL if configured.
		if spec.DefaultURL != "" && spec.SetURL != nil && spec.GetURL != nil {
			if spec.GetURL(cfg) == "" {
				spec.SetURL(&cfg, spec.DefaultURL)
			}
		}

		ApplyEnvOverrides(&cfg, spec.GetName(cfg), spec.EnvPrefix, spec.EnvFields)

		inst, ok := spec.Build(cfg)
		if !ok {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}
