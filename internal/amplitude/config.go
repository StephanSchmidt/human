package amplitude

import (
	"github.com/StephanSchmidt/human/internal/config"
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

// instanceSpec defines how Amplitude configs are loaded and built.
var instanceSpec = config.InstanceSpec[Config, Instance]{
	Section:    "amplitudes",
	EnvPrefix:  "AMPLITUDE_",
	DefaultURL: "https://amplitude.com",
	EnvFields: []config.EnvField[Config]{
		{Suffix: "URL", Set: func(c *Config, v string) { c.URL = v }},
		{Suffix: "KEY", Set: func(c *Config, v string) { c.Key = v }},
		{Suffix: "SECRET", Set: func(c *Config, v string) { c.Secret = v }},
	},
	GetName: func(c Config) string { return c.Name },
	SetURL:  func(c *Config, v string) { c.URL = v },
	GetURL:  func(c Config) string { return c.URL },
	Build: func(cfg Config) (Instance, bool) {
		if cfg.Key == "" || cfg.Secret == "" {
			return Instance{}, false
		}
		return Instance{
			Name:        cfg.Name,
			URL:         cfg.URL,
			Description: cfg.Description,
			Client:      New(cfg.URL, cfg.Key, cfg.Secret),
		}, true
	},
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use Amplitude instances.
func LoadInstances(dir string) ([]Instance, error) {
	return config.LoadInstances(dir, instanceSpec)
}
