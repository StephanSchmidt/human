package amplitude

import (
	"os"
	"strings"

	"github.com/stephanschmidt/human/internal/config"
)

// Config holds the configuration for a single Amplitude instance.
type Config struct {
	Name        string `mapstructure:"name"`
	URL         string `mapstructure:"url"`
	Key         string `mapstructure:"key"`
	Secret      string `mapstructure:"secret"` // #nosec G117 -- config field name, not a secret value
	Description string `mapstructure:"description"`
}

// Instance represents a configured Amplitude instance ready for use.
type Instance struct {
	Name        string
	URL         string
	Description string
	Client      *Client
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Amplitude instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "amplitudes", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use Amplitude instances.
func LoadInstances(dir string) ([]Instance, error) {
	configs, err := LoadConfigs(dir)
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0, len(configs))
	for _, cfg := range configs {
		if cfg.URL == "" {
			cfg.URL = "https://amplitude.com"
		}

		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Key == "" || cfg.Secret == "" {
			continue
		}

		instances = append(instances, Instance{
			Name:        cfg.Name,
			URL:         cfg.URL,
			Description: cfg.Description,
			Client:      New(cfg.URL, cfg.Key, cfg.Secret),
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global AMPLITUDE_URL, AMPLITUDE_KEY,
// AMPLITUDE_SECRET environment variables over any config values.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("AMPLITUDE_URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv("AMPLITUDE_KEY"); ok {
		cfg.Key = v
	}
	if v, ok := os.LookupEnv("AMPLITUDE_SECRET"); ok {
		cfg.Secret = v
	}
}

// applyEnvOverrides checks for per-instance environment variables
// (AMPLITUDE_<UPPER(name)>_URL, _KEY, _SECRET) and overwrites the corresponding
// struct fields when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "AMPLITUDE_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "KEY"); ok {
		cfg.Key = v
	}
	if v, ok := os.LookupEnv(prefix + "SECRET"); ok {
		cfg.Secret = v
	}
}
