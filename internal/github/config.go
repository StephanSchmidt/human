package github

import (
	"os"
	"strings"

	"human/errors"
	"human/internal/config"
	"human/internal/tracker"
)

// Config holds the configuration for a single GitHub instance.
type Config struct {
	Name  string `mapstructure:"name"`
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured GitHub instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "githubs", &configs); err != nil {
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
		if cfg.URL == "" {
			cfg.URL = "https://api.github.com"
		}

		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Token == "" {
			return nil, errors.WithDetails("incomplete github config", "name", cfg.Name)
		}

		instances = append(instances, tracker.Instance{
			Name:     cfg.Name,
			Kind:     "github",
			URL:      cfg.URL,
			Provider: New(cfg.URL, cfg.Token),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global GITHUB_URL, GITHUB_TOKEN
// environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("GITHUB_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("GITHUB_TOKEN"); ok {
		cfg.Token = v
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

// SetupEnv selects a GitHub config by name and sets GITHUB_URL and
// GITHUB_TOKEN environment variables. When name is empty the first entry
// is used. Env vars already set in the environment are not overwritten.
// If no URL is configured, defaults to https://api.github.com.
func SetupEnv(configs []Config, name string) error {
	if len(configs) == 0 {
		return nil
	}

	cfg, err := selectConfig(configs, name)
	if err != nil {
		return err
	}

	if cfg.URL == "" {
		cfg.URL = "https://api.github.com"
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

	return Config{}, errors.WithDetails("unknown github config", "name", name)
}

// applyEnvOverrides checks for per-instance environment variables
// (GITHUB_<UPPER(name)>_URL, _TOKEN) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "GITHUB_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "TOKEN"); ok {
		cfg.Token = v
	}
}

// setEnv sets GITHUB_URL and GITHUB_TOKEN from a Config,
// skipping any variable already present in the environment.
func setEnv(cfg Config) error {
	envMap := map[string]string{
		"GITHUB_URL":   cfg.URL,
		"GITHUB_TOKEN": cfg.Token,
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
