package github

import (
	"os"
	"strings"

	"github.com/stephanschmidt/human/internal/config"
	"github.com/stephanschmidt/human/internal/tracker"
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
			continue
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
