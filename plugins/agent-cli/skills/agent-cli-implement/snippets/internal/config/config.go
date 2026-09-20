// Snippet: internal/config/config.go — file + env + flag cascade.
//
// Cascade: defaults → file → env → flags. Each stage only overrides
// non-empty values. Validate() returns *cliexit.AuthError so the missing-
// token case exits with code 2 from main.go.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/example/acme-cli/internal/cliexit"
	"gopkg.in/yaml.v3"
)

const defaultBaseURL = "https://api.acme.example.com"

// DefaultBaseURL is the compiled-in endpoint, exported so the bootstrap
// can warn when the effective endpoint differs from it.
func DefaultBaseURL() string { return defaultBaseURL }

type Config struct {
	AccessToken string `yaml:"access_token"`
	BaseURL     string `yaml:"base_url"`
	AccountID   string `yaml:"account_id"`
}

// DefaultPath resolves the config location. It returns an error rather
// than a relative path when the home directory cannot be determined: with
// $HOME unset (containers, CI) the old `filepath.Join("", ...)` form wrote
// the access token to ./.config/acme/config.yaml, one `git add -A` away
// from being committed.
func DefaultPath() (string, error) {
	if p := os.Getenv("ACME_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot determine home directory; set ACME_CONFIG to an explicit path")
	}
	return filepath.Join(home, ".config", "acme", "config.yaml"), nil
}

// DefaultPathOrEmpty is the forgiving form for message text, where a
// missing home directory should not itself become an error.
func DefaultPathOrEmpty() string {
	p, err := DefaultPath()
	if err != nil {
		return "<config path unresolved: set ACME_CONFIG>"
	}
	return p
}

// Load reads config from the file at path. Env vars and flags are applied
// separately so the cascade is visible at the call site.
func Load(path string) (*Config, error) {
	cfg, _, err := LoadWithWarning(path)
	return cfg, err
}

// LoadWithWarning also reports a non-fatal warning the caller should print
// to stderr. The permission check matters because the file holds a bearer
// token: Save writes 0600, but a file restored from a backup, copied with
// cp, or produced by a shell redirect arrives at the umask.
func LoadWithWarning(path string) (*Config, string, error) {
	cfg := &Config{BaseURL: defaultBaseURL}
	var warning string
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		warning = fmt.Sprintf("config %s is mode %#o; it holds an access token — run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, warning, nil
		}
		return nil, warning, fmt.Errorf("reading config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, warning, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return cfg, warning, nil
}

func Save(path string, cfg *Config) error {
	// 0700, not 0755: the directory holds a 0600 token file, and a
	// world-listable parent leaks its existence and name.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
//
// The tenant check is conditional: keep it when the API is tenant-scoped
// (a {accountId}-style placeholder in every path), drop it otherwise.
// Without it a missing tenant ID substitutes empty, producing
// `/v1/accounts//projects` and a bare 404 whose hint sends the agent
// chasing a resource ID instead of its own configuration.
//
// The base-URL check binds the credential to an endpoint. The token is
// attached to whatever host is configured, so an unvalidated --base-url is
// a one-step token-exfiltration primitive: injected text in an API
// response that says "this account moved, retry with --base-url ..." is
// all it takes.
func (c *Config) Validate() error {
	if c.AccessToken == "" {
		return &cliexit.AuthError{Err: fmt.Errorf(
			"access_token not configured\n  Set in %s or ACME_ACCESS_TOKEN env var.\n  Run: acme config init",
			DefaultPathOrEmpty())}
	}
	if err := validateBaseURL(c.BaseURL); err != nil {
		return &cliexit.ValidationError{Err: err}
	}
	if c.AccountID == "" {
		return &cliexit.AuthError{Err: fmt.Errorf(
			"account_id not configured\n  Set in %s or ACME_ACCOUNT_ID env var.\n  Run: acme config init",
			DefaultPathOrEmpty())}
	}
	return nil
}

// validateBaseURL requires https, excepting loopback for local development.
func validateBaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("base_url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("base_url %q is not a valid URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || net.ParseIP(host).IsLoopback() {
			return nil
		}
		return fmt.Errorf("base_url %q uses http; the access token would be sent in cleartext (only loopback may use http)", raw)
	default:
		return fmt.Errorf("base_url %q must use https", raw)
	}
}
