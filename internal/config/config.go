// Package config loads Zeuf's user configuration. Secrets are never
// stored here: direct endpoints reference environment variables, and the
// opencode/kilo gateways reuse the user's own CLI logins.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"zeuf/internal/router"
)

// DirectEndpoint configures one OpenAI-compatible backend.
type DirectEndpoint struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
}

// Config is the user configuration file.
type Config struct {
	BackendsOrder []string         `json:"backends_order,omitempty"`
	Direct        []DirectEndpoint `json:"direct,omitempty"`
	OpencodeServe string           `json:"opencode_serve_url,omitempty"`
	KiloServe     string           `json:"kilo_serve_url,omitempty"`
	Prefs         router.Prefs     `json:"prefs"`
	AutoApprove   bool             `json:"auto_approve"`
	Workdir       string           `json:"workdir,omitempty"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		BackendsOrder: []string{"opencode", "kilo", "direct"},
		Prefs:         router.DefaultPrefs(),
	}
}

// Path returns the config file path (XDG_CONFIG_HOME or ~/.config).
func Path() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "zeuf", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zeuf", "config.json")
}

// Load reads the config file, creating defaults on first run.
func Load() (Config, error) {
	cfg := Default()
	p := Path()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse config %s: %w", p, err)
	}
	if cfg.Prefs.MaxAttempts == 0 {
		cfg.Prefs.MaxAttempts = 4
	}
	return cfg, nil
}

// Save writes the config file (mode 0600; contains no secrets by design).
func Save(cfg Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}
