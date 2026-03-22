package azuredevops

import (
	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// Config holds the configuration for a single Azure DevOps instance.
type Config struct {
	Name        string `mapstructure:"name"`
	URL         string `mapstructure:"url"`
	Org         string `mapstructure:"org"`
	Token       string `mapstructure:"token"`
	Description string `mapstructure:"description"`
	Safe        bool   `mapstructure:"safe"`
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Azure DevOps instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "azuredevops", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// instanceSpec defines how Azure DevOps configs are loaded and built.
var instanceSpec = config.InstanceSpec[Config, tracker.Instance]{
	Section:    "azuredevops",
	EnvPrefix:  "AZURE_",
	DefaultURL: "https://dev.azure.com",
	EnvFields: []config.EnvField[Config]{
		{Suffix: "URL", Set: func(c *Config, v string) { c.URL = v }},
		{Suffix: "ORG", Set: func(c *Config, v string) { c.Org = v }},
		{Suffix: "TOKEN", Set: func(c *Config, v string) { c.Token = v }},
	},
	GetName: func(c Config) string { return c.Name },
	SetURL:  func(c *Config, v string) { c.URL = v },
	GetURL:  func(c Config) string { return c.URL },
	Build: func(cfg Config) (tracker.Instance, bool) {
		if cfg.Token == "" || cfg.Org == "" {
			return tracker.Instance{}, false
		}
		return tracker.Instance{
			Name:        cfg.Name,
			Kind:        "azuredevops",
			URL:         cfg.URL,
			Description: cfg.Description,
			Safe:        cfg.Safe,
			Provider:    New(cfg.URL, cfg.Org, cfg.Token),
		}, true
	},
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use tracker instances.
func LoadInstances(dir string) ([]tracker.Instance, error) {
	return config.LoadInstances(dir, instanceSpec)
}
