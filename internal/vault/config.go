package vault

import (
	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/platform"
)

// Config holds the vault configuration from .humanconfig.
type Config struct {
	Provider string `mapstructure:"provider"`
	Account  string `mapstructure:"account"`
}

// ReadConfig reads the vault section from .humanconfig in dir.
// Returns nil if no vault section is present or the file is missing.
func ReadConfig(dir string) *Config {
	var cfg Config
	if err := config.UnmarshalSection(dir, "vault", &cfg); err != nil {
		return nil
	}
	if cfg.Provider == "" {
		return nil
	}
	return &cfg
}

// NewResolverFromConfig creates a Resolver based on the vault configuration.
// Returns nil if cfg is nil or the provider is unrecognized (graceful no-op).
func NewResolverFromConfig(cfg *Config) *Resolver {
	if cfg == nil {
		return nil
	}

	switch cfg.Provider {
	case "1password", "1pw":
		if platform.IsWSL() {
			return NewResolver(NewOpCLI())
		}
		return NewResolver(NewOnePassword(cfg.Account))
	default:
		return nil
	}
}
