package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"

	"human/errors"
)

// JiraConfig holds the configuration for a single Jira instance.
type JiraConfig struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
	User string `mapstructure:"user"`
	Key  string `mapstructure:"key"`
}

// LoadJiraConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured Jira instances. Returns nil and no error if the file
// does not exist.
func LoadJiraConfigs(dir string) ([]JiraConfig, error) {
	v, err := readConfig(dir)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}

	var configs []JiraConfig
	if err := v.UnmarshalKey("jiras", &configs); err != nil {
		return nil, errors.WrapWithDetails(err, "parsing jiras config", "dir", dir)
	}

	return configs, nil
}

// LoadConfig reads a .humanconfig YAML file from dir and sets JIRA_URL,
// JIRA_USER, and JIRA_KEY environment variables from the selected Jira
// entry. When name is empty the first entry is used; otherwise the entry
// with the matching name is selected. Env vars already set in the
// environment are not overwritten. Missing config files are silently ignored.
func LoadConfig(dir, name string) error {
	configs, err := LoadJiraConfigs(dir)
	if err != nil {
		return err
	}

	if len(configs) == 0 {
		return nil
	}

	cfg, err := selectJira(configs, name)
	if err != nil {
		return err
	}

	applyEnvOverrides(&cfg)

	return setJiraEnv(cfg)
}

// readConfig creates a viper instance and reads the .humanconfig file from
// dir. Returns (nil, nil) when no config file exists.
func readConfig(dir string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigName(".humanconfig")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, nil
		}
		return nil, errors.WrapWithDetails(err, "parsing config file", "dir", dir)
	}

	return v, nil
}

// selectJira picks a JiraConfig by name, or returns the first entry when
// name is empty.
func selectJira(configs []JiraConfig, name string) (JiraConfig, error) {
	if name == "" {
		return configs[0], nil
	}

	for _, c := range configs {
		if c.Name == name {
			return c, nil
		}
	}

	return JiraConfig{}, errors.WithDetails("unknown jira config", "name", name)
}

// applyEnvOverrides checks for per-instance environment variables
// (JIRA_<UPPER(name)>_URL, _USER, _KEY) and overwrites the corresponding
// struct fields when set. This allows instance-specific secrets to live in
// the environment rather than in .humanconfig.
func applyEnvOverrides(cfg *JiraConfig) {
	if cfg.Name == "" {
		return
	}
	prefix := "JIRA_" + strings.ToUpper(cfg.Name) + "_"
	if v, ok := os.LookupEnv(prefix + "URL"); ok {
		cfg.URL = v
	}
	if v, ok := os.LookupEnv(prefix + "USER"); ok {
		cfg.User = v
	}
	if v, ok := os.LookupEnv(prefix + "KEY"); ok {
		cfg.Key = v
	}
}

// setJiraEnv sets JIRA_URL, JIRA_USER, and JIRA_KEY from a JiraConfig,
// skipping any variable already present in the environment.
func setJiraEnv(cfg JiraConfig) error {
	envMap := map[string]string{
		"JIRA_URL":  cfg.URL,
		"JIRA_USER": cfg.User,
		"JIRA_KEY":  cfg.Key,
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
