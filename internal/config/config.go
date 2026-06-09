// Package config loads tractor-cli configuration from YAML and environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration for tractor-cli.
type Config struct {
	Engine  EngineConfig  `mapstructure:"engine"`
	Auth    AuthConfig    `mapstructure:"auth"`
	UI      UIConfig      `mapstructure:"ui"`
	Filters FiltersConfig `mapstructure:"filters"`
}

// EngineConfig holds Tractor engine connection settings.
type EngineConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// AuthConfig holds authentication settings.
// Password is intentionally not stored here — use TRACTOR_PASSWORD env var.
type AuthConfig struct {
	Username string `mapstructure:"username"`
}

// UIConfig holds display and behaviour settings.
type UIConfig struct {
	RefreshInterval int    `mapstructure:"refresh_interval"`
	DefaultView     string `mapstructure:"default_view"`
	DateFormat      string `mapstructure:"date_format"`
	MaxLogLines     int    `mapstructure:"max_log_lines"`
}

// FiltersConfig holds default filter settings applied on launch.
type FiltersConfig struct {
	DefaultOwner string `mapstructure:"default_owner"`
}

// Load reads configuration from ~/.config/tractor-cli/config.yaml and
// environment variable overrides (TRACTOR_HOST, TRACTOR_PORT, TRACTOR_USER,
// TRACTOR_PASSWORD).
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("engine.host", "localhost")
	v.SetDefault("engine.port", 80)
	v.SetDefault("ui.refresh_interval", 30)
	v.SetDefault("ui.default_view", "jobs")
	v.SetDefault("ui.date_format", "15:04 02/01")
	v.SetDefault("ui.max_log_lines", 500)

	// Config file location
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine config dir: %w", err)
	}
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(filepath.Join(cfgDir, "tractor-cli"))

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		// Config file is optional; continue with defaults + env vars.
	}

	// Environment variable overrides
	v.SetEnvPrefix("")
	v.BindEnv("engine.host", "TRACTOR_HOST")
	v.BindEnv("engine.port", "TRACTOR_PORT")
	v.BindEnv("auth.username", "TRACTOR_USER")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	// Username fallback: OS user
	if cfg.Auth.Username == "" {
		cfg.Auth.Username = os.Getenv("USER")
	}

	return &cfg, nil
}

// Password returns the Tractor password from the TRACTOR_PASSWORD environment
// variable. It is intentionally never read from the config file.
func Password() string {
	return os.Getenv("TRACTOR_PASSWORD")
}

// BaseURL returns the full base URL for the Tractor engine API.
func (c *Config) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", c.Engine.Host, c.Engine.Port)
}
