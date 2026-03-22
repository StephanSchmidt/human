package telegram

import (
	"github.com/StephanSchmidt/human/internal/config"
)

// Config holds the configuration for a single Telegram bot instance.
type Config struct {
	Name         string  `mapstructure:"name"`
	Token        string  `mapstructure:"token"`
	Description  string  `mapstructure:"description"`
	AllowedUsers []int64 `mapstructure:"allowed_users"`
}

// Instance represents a configured Telegram bot ready for use.
type Instance struct {
	Name         string
	Description  string
	Client       *Client
	AllowedUsers []int64
}

// LoadConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Telegram instances. Returns nil and no error if the file
// does not exist.
func LoadConfigs(dir string) ([]Config, error) {
	var configs []Config
	if err := config.UnmarshalSection(dir, "telegrams", &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// instanceSpec defines how Telegram configs are loaded and built.
var instanceSpec = config.InstanceSpec[Config, Instance]{
	Section:   "telegrams",
	EnvPrefix: "TELEGRAM_",
	EnvFields: []config.EnvField[Config]{
		{Suffix: "TOKEN", Set: func(c *Config, v string) { c.Token = v }},
	},
	GetName: func(c Config) string { return c.Name },
	Build: func(cfg Config) (Instance, bool) {
		if cfg.Token == "" {
			return Instance{}, false
		}
		return Instance{
			Name:         cfg.Name,
			Description:  cfg.Description,
			Client:       New(cfg.Token),
			AllowedUsers: cfg.AllowedUsers,
		}, true
	},
}

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use Telegram instances.
func LoadInstances(dir string) ([]Instance, error) {
	return config.LoadInstances(dir, instanceSpec)
}
