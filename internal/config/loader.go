package config

import (
	"os"
	"strings"
)

// EnvLookup is a function that looks up an environment variable by key.
// It returns the value and whether the variable was found,
// matching the signature of os.LookupEnv.
type EnvLookup func(key string) (string, bool)

// EnvField maps a config struct field to its environment variable suffix.
// For example, {Suffix: "TOKEN", Set: func(c *MyConfig, v string) { c.Token = v }}
// will check for PROVIDER_TOKEN (global) and PROVIDER_NAME_TOKEN (per-instance).
type EnvField[C any] struct {
	Suffix string
	Set    func(c *C, v string)
}

// ApplyEnvOverrides applies environment variable overrides to a config struct.
// It checks global variables first (PREFIX_SUFFIX), then per-instance variables
// (PREFIX_NAME_SUFFIX). Per-instance overrides take precedence.
//
// The lookup parameter controls how environment variables are resolved.
// When nil, os.LookupEnv is used. Pass a custom function to implement
// per-project scoping or other lookup strategies.
func ApplyEnvOverrides[C any](cfg *C, name, envPrefix string, fields []EnvField[C], lookup EnvLookup) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	// Global overrides: PREFIX_SUFFIX
	for _, f := range fields {
		if v, ok := lookup(envPrefix + f.Suffix); ok {
			f.Set(cfg, v)
		}
	}

	// Per-instance overrides: PREFIX_NAME_SUFFIX (takes precedence)
	if name != "" {
		instancePrefix := envPrefix + strings.ToUpper(name) + "_"
		for _, f := range fields {
			if v, ok := lookup(instancePrefix + f.Suffix); ok {
				f.Set(cfg, v)
			}
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

	// Lookup overrides how environment variables are resolved.
	// When nil, os.LookupEnv is used. Set this to a per-project scoped
	// lookup function to support multi-project token isolation.
	Lookup EnvLookup

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

		ApplyEnvOverrides(&cfg, spec.GetName(cfg), spec.EnvPrefix, spec.EnvFields, spec.Lookup)

		inst, ok := spec.Build(cfg)
		if !ok {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}
