// Snippet: internal/config/config.go — file + env + flag cascade.
//
// Cascade: defaults → file → env → flags. Each stage only overrides
// non-empty values. Validate() returns *cliexit.AuthError so the missing-
// token case exits with code 2 from main.go.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/example/acme-cli/internal/cliexit"
	"gopkg.in/yaml.v3"
)

const defaultBaseURL = "https://api.acme.example.com"

type Config struct {
	AccessToken string `yaml:"access_token"`
	BaseURL     string `yaml:"base_url"`
	AccountID   string `yaml:"account_id"`
}

func DefaultPath() string {
	if p := os.Getenv("ACME_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "acme", "config.yaml")
}

// Load reads config from the file at path. Env vars and flags are applied
// separately so the cascade is visible at the call site.
func Load(path string) (*Config, error) {
	cfg := &Config{BaseURL: defaultBaseURL}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

func (c *Config) ApplyEnv() {
	if v := os.Getenv("ACME_ACCESS_TOKEN"); v != "" {
		c.AccessToken = v
	}
	if v := os.Getenv("ACME_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("ACME_ACCOUNT_ID"); v != "" {
		c.AccountID = v
	}
}

func (c *Config) ApplyFlags(token, baseURL, accountID string) {
	if token != "" {
		c.AccessToken = token
	}
	if baseURL != "" {
		c.BaseURL = baseURL
	}
	if accountID != "" {
		c.AccountID = accountID
	}
}

// Validate returns *cliexit.AuthError so missing-token exits with code 2.
func (c *Config) Validate() error {
	if c.AccessToken == "" {
		return &cliexit.AuthError{Err: fmt.Errorf(
			"access_token not configured\n  Set in %s or ACME_ACCESS_TOKEN env var.\n  Run: acme config init",
			DefaultPath())}
	}
	return nil
}
