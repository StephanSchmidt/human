package config

import (
	"os"

	"github.com/spf13/viper"

	"human/errors"
)

// configMapping maps Viper keys (from .humanconfig YAML) to environment variable names.
var configMapping = map[string]string{
	"jira.url":  "JIRA_URL",
	"jira.user": "JIRA_USER",
	"jira.key":  "JIRA_KEY",
}

// LoadConfig reads a .humanconfig YAML file from dir and sets environment
// variables for any config values not already present in the environment.
// Missing config files are silently ignored.
func LoadConfig(dir string) error {
	v := viper.New()
	v.SetConfigName(".humanconfig")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil
		}
		return errors.WrapWithDetails(err, "parsing config file", "dir", dir)
	}

	return setEnvFromConfig(v, configMapping)
}

// setEnvFromConfig sets environment variables from Viper values for keys that
// are not already set in the environment.
func setEnvFromConfig(v *viper.Viper, mapping map[string]string) error {
	for viperKey, envVar := range mapping {
		if _, exists := os.LookupEnv(envVar); exists {
			continue
		}
		val := v.GetString(viperKey)
		if val == "" {
			continue
		}
		if err := os.Setenv(envVar, val); err != nil {
			return errors.WrapWithDetails(err, "setting env var", "key", envVar)
		}
	}
	return nil
}
