package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Addendum A: a configured tenant placeholder with no tenant ID must be an
// actionable auth/config error, not a silently malformed URL.
func TestValidateRequiresTenantWhenScoped(t *testing.T) {
	c := &Config{AccessToken: "tok", BaseURL: "https://x", AccountID: ""}
	if err := c.Validate(); err == nil {
		t.Error("expected missing tenant ID to be rejected")
	} else if !strings.Contains(err.Error(), "ACME_ACCOUNT_ID") {
		t.Errorf("error should name the env var, got: %v", err)
	}
}

func TestValidateAcceptsComplete(t *testing.T) {
	c := &Config{AccessToken: "tok", BaseURL: "https://x", AccountID: "acct"}
	if err := c.Validate(); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

// Security #3: the token attaches to whatever base URL is configured, so a
// plaintext or malformed endpoint must be refused.
func TestValidateRejectsInsecureBaseURL(t *testing.T) {
	for _, u := range []string{"http://evil.example", "ftp://x", "not-a-url"} {
		c := &Config{AccessToken: "tok", BaseURL: u, AccountID: "acct"}
		if err := c.Validate(); err == nil {
			t.Errorf("expected base URL %q to be rejected", u)
		}
	}
}

func TestValidateAllowsLocalhostOverHTTP(t *testing.T) {
	c := &Config{AccessToken: "tok", BaseURL: "http://localhost:8080", AccountID: "acct"}
	if err := c.Validate(); err != nil {
		t.Errorf("localhost should be allowed for development: %v", err)
	}
}

// Finding #12: with HOME unset the token must not land in the CWD.
func TestDefaultPathIsAbsolute(t *testing.T) {
	t.Setenv("ACME_CONFIG", "")
	t.Setenv("HOME", "")
	p, err := DefaultPath()
	if err == nil && !filepath.IsAbs(p) {
		t.Errorf("DefaultPath returned relative path %q with HOME unset", p)
	}
}

// Finding #12 / security #9: a group- or world-readable token file is a
// misconfiguration the CLI should not consume silently.
func TestLoadWarnsOnLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("access_token: tok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warn, err := LoadWithWarning(p)
	if err != nil {
		t.Fatal(err)
	}
	if warn == "" {
		t.Error("expected a permissions warning for a 0644 token file")
	}
}
