package telegram

import (
	"os"
	"strings"

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

// LoadInstances reads config, applies env overrides, creates clients,
// and returns ready-to-use Telegram instances.
func LoadInstances(dir string) ([]Instance, error) {
	configs, err := LoadConfigs(dir)
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0, len(configs))
	for _, cfg := range configs {
		applyEnvOverrides(&cfg)
		applyGlobalEnvOverrides(&cfg)

		if cfg.Token == "" {
			continue
		}

		instances = append(instances, Instance{
			Name:         cfg.Name,
			Description:  cfg.Description,
			Client:       New(cfg.Token),
			AllowedUsers: cfg.AllowedUsers,
		})
	}
	return instances, nil
}

// applyGlobalEnvOverrides applies global TELEGRAM_TOKEN
// environment variable over any config value.
func applyGlobalEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("TELEGRAM_TOKEN"); ok {
		cfg.Token = v
	}
}

// applyEnvOverrides checks for per-instance environment variables
// (TELEGRAM_<UPPER(name)>_TOKEN) and overwrites the corresponding
// struct field when set.
func applyEnvOverrides(cfg *Config) {
	if cfg.Name == "" {
		return
	}
	prefix := "TELEGRAM_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "TOKEN"); ok {
		cfg.Token = v
	}
}
