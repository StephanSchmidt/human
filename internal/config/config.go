package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"

	"human/errors"
)

// TrackerKind identifies which issue tracker backend a config entry belongs to.
type TrackerKind int

const (
	TrackerJira   TrackerKind = iota
	TrackerGitHub
)

// JiraConfig holds the configuration for a single Jira instance.
type JiraConfig struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
	User string `mapstructure:"user"`
	Key  string `mapstructure:"key"`
}

// GitHubConfig holds the configuration for a single GitHub instance.
type GitHubConfig struct {
	Name  string `mapstructure:"name"`
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
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

// LoadGitHubConfigs reads a .humanconfig YAML file from dir and returns the
// list of configured GitHub instances. Returns nil and no error if the file
// does not exist.
func LoadGitHubConfigs(dir string) ([]GitHubConfig, error) {
	v, err := readConfig(dir)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}

	var configs []GitHubConfig
	if err := v.UnmarshalKey("githubs", &configs); err != nil {
		return nil, errors.WrapWithDetails(err, "parsing githubs config", "dir", dir)
	}

	return configs, nil
}

// LoadGitHubConfig reads a .humanconfig YAML file from dir and sets
// GITHUB_URL and GITHUB_TOKEN environment variables from the selected
// GitHub entry. When name is empty the first entry is used; otherwise the
// entry with the matching name is selected. Env vars already set in the
// environment are not overwritten. Missing config files are silently ignored.
func LoadGitHubConfig(dir, name string) error {
	configs, err := LoadGitHubConfigs(dir)
	if err != nil {
		return err
	}

	if len(configs) == 0 {
		return nil
	}

	cfg, err := selectGitHub(configs, name)
	if err != nil {
		return err
	}

	if cfg.URL == "" {
		cfg.URL = "https://api.github.com"
	}

	applyGitHubEnvOverrides(&cfg)

	return setGitHubEnv(cfg)
}

// selectGitHub picks a GitHubConfig by name, or returns the first entry when
// name is empty.
func selectGitHub(configs []GitHubConfig, name string) (GitHubConfig, error) {
	if name == "" {
		return configs[0], nil
	}

	for _, c := range configs {
		if c.Name == name {
			return c, nil
		}
	}

	return GitHubConfig{}, errors.WithDetails("unknown github config", "name", name)
}

// applyGitHubEnvOverrides checks for per-instance environment variables
// (GITHUB_<UPPER(name)>_URL, _TOKEN) and overwrites the corresponding
// struct fields when set.
func applyGitHubEnvOverrides(cfg *GitHubConfig) {
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

// setGitHubEnv sets GITHUB_URL and GITHUB_TOKEN from a GitHubConfig,
// skipping any variable already present in the environment.
func setGitHubEnv(cfg GitHubConfig) error {
	envMap := map[string]string{
		"GITHUB_URL":   cfg.URL,
		"GITHUB_TOKEN": cfg.Token,
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

// ResolveTracker determines which tracker backend to use based on the
// given name and the contents of .humanconfig in dir.
//
// When name is provided it searches both jiras and githubs sections.
// When name is empty it auto-detects: if only one type is configured it
// uses that type; if both are present it returns an error.
//
// On success the corresponding LoadConfig/LoadGitHubConfig is called to
// populate environment variables.
func ResolveTracker(dir, name string) (TrackerKind, error) {
	jiraConfigs, err := LoadJiraConfigs(dir)
	if err != nil {
		return 0, err
	}
	ghConfigs, err := LoadGitHubConfigs(dir)
	if err != nil {
		return 0, err
	}

	if name != "" {
		inJira := containsJiraName(jiraConfigs, name)
		inGitHub := containsGitHubName(ghConfigs, name)

		if inJira && inGitHub {
			return 0, errors.WithDetails("ambiguous tracker name found in both jiras and githubs sections", "name", name)
		}
		if inJira {
			if err := LoadConfig(dir, name); err != nil {
				return 0, err
			}
			return TrackerJira, nil
		}
		if inGitHub {
			if err := LoadGitHubConfig(dir, name); err != nil {
				return 0, err
			}
			return TrackerGitHub, nil
		}
		return 0, errors.WithDetails("tracker name not found in .humanconfig", "name", name)
	}

	hasJira := len(jiraConfigs) > 0
	hasGitHub := len(ghConfigs) > 0

	if hasJira && hasGitHub {
		return 0, errors.WithDetails("multiple tracker types configured, specify --tracker=<name>")
	}
	if hasJira {
		if err := LoadConfig(dir, ""); err != nil {
			return 0, err
		}
		return TrackerJira, nil
	}
	if hasGitHub {
		if err := LoadGitHubConfig(dir, ""); err != nil {
			return 0, err
		}
		return TrackerGitHub, nil
	}

	return 0, errors.WithDetails("no tracker configured, add jiras: or githubs: to .humanconfig.yaml")
}

// containsJiraName reports whether any JiraConfig in the slice has the given name.
func containsJiraName(configs []JiraConfig, name string) bool {
	for _, c := range configs {
		if c.Name == name {
			return true
		}
	}
	return false
}

// containsGitHubName reports whether any GitHubConfig in the slice has the given name.
func containsGitHubName(configs []GitHubConfig, name string) bool {
	for _, c := range configs {
		if c.Name == name {
			return true
		}
	}
	return false
}
