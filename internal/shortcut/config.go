package shortcut

import (
	"os"
	"strings"

	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// Config holds the configuration for a single Shortcut instance.
type Config struct {
	Name        string `mapstructure:"name"`
	URL         string `mapstructure:"url"`
	Token       string `mapstructure:"token"`
	Description string `mapstructure:"description"`
	Safe        bool   `mapstructure:"safe"`
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Shortcut instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "shortcuts", &configs); err != nil {
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
			cfg.URL = "https://api.app.shortcut.com"
		}

		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Token == "" {
			continue
		}

		instances = append(instances, tracker.Instance{
			Name:        cfg.Name,
			Kind:        "shortcut",
			URL:         cfg.URL,
			Description: cfg.Description,
			Safe:        cfg.Safe,
			Provider:    New(cfg.URL, cfg.Token),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global SHORTCUT_URL, SHORTCUT_TOKEN
// environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("SHORTCUT_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("SHORTCUT_TOKEN"); ok {
		cfg.Token = v
	}
}

// applyEnvOverrides checks for per-instance environment variables
// (SHORTCUT_<UPPER(name)>_URL, _TOKEN) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "SHORTCUT_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "TOKEN"); ok {
		cfg.Token = v
	}
}
