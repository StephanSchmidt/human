package notion

import (
	"os"
	"strings"

	"github.com/stephanschmidt/human/internal/config"
)

// Config holds the configuration for a single Notion instance.
type Config struct {
	Name        string `mapstructure:"name"`
	URL         string `mapstructure:"url"`
	Token       string `mapstructure:"token"`
	Description string `mapstructure:"description"`
}

// Instance represents a configured Notion workspace ready for use.
type Instance struct {
	Name        string
	URL         string
	Description string
	Client      *Client
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Notion instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "notions", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use Notion instances.
func LoadInstances(dir string) ([]Instance, error) {
	configs, err := LoadConfigs(dir)
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0, len(configs))
	for _, cfg := range configs {
		if cfg.URL == "" {
			cfg.URL = "https://api.notion.com"
		}

		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Token == "" {
			continue
		}

		instances = append(instances, Instance{
			Name:        cfg.Name,
			URL:         cfg.URL,
			Description: cfg.Description,
			Client:      New(cfg.URL, cfg.Token),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global NOTION_URL, NOTION_TOKEN
// environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("NOTION_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("NOTION_TOKEN"); ok {
		cfg.Token = v
	}
}

// applyEnvOverrides checks for per-instance environment variables
// (NOTION_<UPPER(name)>_URL, _TOKEN) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "NOTION_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "TOKEN"); ok {
		cfg.Token = v
	}
}
