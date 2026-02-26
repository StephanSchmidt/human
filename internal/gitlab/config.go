package gitlab

import (
	"os"
	"strings"

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/config"
	"github.com/stephanschmidt/human/internal/tracker"
)

// Config holds the configuration for a single GitLab instance.
type Config struct {
	Name  string `mapstructure:"name"`
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured GitLab instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "gitlabs", &configs); err != nil {
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
			cfg.URL = "https://gitlab.com"
		}

		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Token == "" {
			return nil, errors.WithDetails("incomplete gitlab config", "name", cfg.Name)
		}

		instances = append(instances, tracker.Instance{
			Name:     cfg.Name,
			Kind:     "gitlab",
			URL:      cfg.URL,
			Provider: New(cfg.URL, cfg.Token),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global GITLAB_URL, GITLAB_TOKEN
// environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("GITLAB_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("GITLAB_TOKEN"); ok {
		cfg.Token = v
	}
}

// applyEnvOverrides checks for per-instance environment variables
// (GITLAB_<UPPER(name)>_URL, _TOKEN) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "GITLAB_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "TOKEN"); ok {
		cfg.Token = v
	}
}
