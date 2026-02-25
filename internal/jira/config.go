package jira

import (
	"os"
	"strings"

	"human/errors"
	"human/internal/config"
	"human/internal/tracker"
)

// Config holds the configuration for a single Jira instance.
type Config struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
	User string `mapstructure:"user"`
	Key  string `mapstructure:"key"`
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Jira instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "jiras", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use tracker instances.
func LoadInstances(dir string) ([]tracker.Instance, error) {
	configs, err := LoadConfigs(dir)
	if err != nil {
		return nil, err
	}

	instances := make([]tracker.Instance, 0, len(configs))
	for _, cfg := range configs {
		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.URL == "" || cfg.User == "" || cfg.Key == "" {
			return nil, errors.WithDetails("incomplete jira config", "name", cfg.Name)
		}

		instances = append(instances, tracker.Instance{
			Name:     cfg.Name,
			Kind:     "jira",
			URL:      cfg.URL,
			User:     cfg.User,
			Provider: New(cfg.URL, cfg.User, cfg.Key),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global JIRA_URL, JIRA_USER, JIRA_KEY
// environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("JIRA_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("JIRA_USER"); ok {
		cfg.User = v
	}
	if v, ok := os.LookupEnv("JIRA_KEY"); ok {
		cfg.Key = v
	}
}

// Names returns the name of each config entry.
func Names(configs []Config) []string {
	names := make([]string, len(configs))
	for i, c := range configs {
		names[i] = c.Name
	}
	return names
}

// SetupEnv selects a Jira config by name and sets JIRA_URL, JIRA_USER,
// and JIRA_KEY environment variables. When name is empty the first entry
// is used. Env vars already set in the environment are not overwritten.
func SetupEnv(configs []Config, name string) error {
	if len(configs) == 0 {
		return nil
	}

	cfg, err := selectConfig(configs, name)
	if err != nil {
		return err
	}

	applyEnvOverrides(&cfg)

	return setEnv(cfg)
}

// selectConfig picks a Config by name, or returns the first entry when
// name is empty.
func selectConfig(configs []Config, name string) (Config, error) {
	if name == "" {
		return configs[0], nil
	}

	for _, c := range configs {
		if c.Name == name {
			return c, nil
		}
	}

	return Config{}, errors.WithDetails("unknown jira config", "name", name)
}

// applyEnvOverrides checks for per-instance environment variables
// (JIRA_<UPPER(name)>_URL, _USER, _KEY) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "JIRA_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "USER"); ok {
		cfg.User = v
	}
	if v, ok := os.LookupEnv(prefix + "KEY"); ok {
		cfg.Key = v
	}
}

// setEnv sets JIRA_URL, JIRA_USER, and JIRA_KEY from a Config,
// skipping any variable already present in the environment.
func setEnv(cfg Config) error {
	envMap := map[string]string{
		"JIRA_URL":  cfg.URL,
		"JIRA_USER": cfg.User,
		"JIRA_KEY":  cfg.Key,
	}

	for envVar, val := range envMap {
		if _, exists := os.LookupEnv(envVar); exists {
			continue
		}
		if val == "" {
			continue
		}
		if err := os.Setenv(envVar, val); err != nil {
			return errors.WrapWithDetails(err, "setting env var", "key", envVar)
		}
	}

	return nil
}
