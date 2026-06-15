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
	LDAP    LDAPConfig    `mapstructure:"ldap"`
	Keys    KeymapConfig  `mapstructure:"keys"`
}

// KeymapConfig lets users rebind the global shortcuts. Defaults are filled in
// by Load. View-internal keys (sort numbers, filter form, etc.) are fixed.
type KeymapConfig struct {
	Quit       string `mapstructure:"quit"`
	Help       string `mapstructure:"help"`
	Actions    string `mapstructure:"actions"`
	Blades     string `mapstructure:"blades"`
	Notes      string `mapstructure:"notes"`
	Trace      string `mapstructure:"trace"`
	Yank       string `mapstructure:"yank"`
	SaveFilter string `mapstructure:"save_filter"`
	LoadFilter string `mapstructure:"load_filter"`
	Next       string `mapstructure:"next"`
	Prev       string `mapstructure:"prev"`
}

// LDAPConfig holds settings for the Active Directory query that populates
// owner-filter autocomplete. Defaults are filled in by Load.
type LDAPConfig struct {
	URI    string `mapstructure:"uri"`     // ldapsearch -H value
	BaseDN string `mapstructure:"base_dn"` // search base
	Realm  string `mapstructure:"realm"`   // Kerberos realm for the kinit principal
	// AutoKinit makes the app attempt `kinit` with TRACTOR_PASSWORD when no
	// Kerberos ticket exists. Off by default: a wrong password can trip an AD
	// account-lockout policy, and a ticket is usually already present via SSSD.
	AutoKinit bool `mapstructure:"auto_kinit"`
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
	// LDAP settings (uri/base_dn/realm) are intentionally left blank — each user
	// supplies their own site values via config.yaml or the TRACTOR_LDAP_* env
	// vars. When unset, owner autocomplete skips LDAP and uses getent + job
	// owners instead.
	v.SetDefault("ldap.auto_kinit", false)
	v.SetDefault("keys.quit", "q")
	v.SetDefault("keys.help", "?")
	v.SetDefault("keys.actions", "a")
	v.SetDefault("keys.blades", "b")
	v.SetDefault("keys.notes", "n")
	v.SetDefault("keys.trace", "t")
	v.SetDefault("keys.yank", "y")
	v.SetDefault("keys.save_filter", "s")
	v.SetDefault("keys.load_filter", "f")
	v.SetDefault("keys.next", "]")
	v.SetDefault("keys.prev", "[")

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
	v.BindEnv("ldap.uri", "TRACTOR_LDAP_URI")
	v.BindEnv("ldap.base_dn", "TRACTOR_LDAP_BASE_DN")
	v.BindEnv("ldap.realm", "TRACTOR_LDAP_REALM")
	v.BindEnv("ldap.auto_kinit", "TRACTOR_LDAP_AUTO_KINIT")

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
