package commands

import (
	"os"
	"strings"
	"testing"
)

// TestResolveAuthSiteURL_Default covers the unset case.
func TestResolveAuthSiteURL_Default(t *testing.T) {
	t.Setenv("FF_AUTH_SITE_URL", "")
	t.Setenv("FF_API_URL", "")
	got := resolveAuthSiteURL()
	if got != "https://functionfly.com" {
		t.Errorf("default auth site = %q, want https://functionfly.com", got)
	}
}

// TestResolveAuthSiteURL_FromEnv covers an explicit override.
func TestResolveAuthSiteURL_FromEnv(t *testing.T) {
	t.Setenv("FF_AUTH_SITE_URL", "https://staging.functionfly.com")
	t.Setenv("FF_API_URL", "")
	got := resolveAuthSiteURL()
	if got != "https://staging.functionfly.com" {
		t.Errorf("env override = %q, want https://staging.functionfly.com", got)
	}
}

// TestResolveAuthSiteURL_StripsTrailingSlash covers the trailing-slash normalization.
func TestResolveAuthSiteURL_StripsTrailingSlash(t *testing.T) {
	t.Setenv("FF_AUTH_SITE_URL", "https://staging.functionfly.com/")
	t.Setenv("FF_API_URL", "")
	got := resolveAuthSiteURL()
	if got != "https://staging.functionfly.com" {
		t.Errorf("trailing slash not stripped: %q", got)
	}
}

// TestResolveAuthSiteURL_DerivedFromLocalAPI covers the localhost derivation.
func TestResolveAuthSiteURL_DerivedFromLocalAPI(t *testing.T) {
	t.Setenv("FF_AUTH_SITE_URL", "")
	cases := []struct {
		api  string
		want string
	}{
		{"http://localhost:8080", "http://localhost:3000"},
		{"http://127.0.0.1:8080", "http://localhost:3000"},
		{"https://api.functionfly.com", "https://functionfly.com"},
	}
	for _, c := range cases {
		t.Setenv("FF_API_URL", c.api)
		got := resolveAuthSiteURL()
		if got != c.want {
			t.Errorf("api=%q → %q, want %q", c.api, got, c.want)
		}
	}
}

// TestResolveAuthSiteURL_RejectsInvalid covers malformed values.
func TestResolveAuthSiteURL_RejectsInvalid(t *testing.T) {
	t.Setenv("FF_AUTH_SITE_URL", "not a url with spaces")
	t.Setenv("FF_API_URL", "")
	got := resolveAuthSiteURL()
	if got != "https://functionfly.com" {
		t.Errorf("invalid URL should fall back to default, got %q", got)
	}
}

// TestValidatePastedToken covers the token length and empty checks.
func TestValidatePastedToken(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"too short", "abc", true},
		{"just under threshold", "1234567", true},
		{"at threshold", strings.Repeat("a", minAccessTokenLen), false},
		{"valid jwt-like", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1In0.abc", false},
		{"trims spaces", "  " + strings.Repeat("a", 20) + "  ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePastedToken(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("validatePastedToken(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
		})
	}
}

// TestTokenRateLimit_AllowsUpToLimit covers the in-process rate limiter.
func TestTokenRateLimit_AllowsUpToLimit(t *testing.T) {
	// Reset global state.
	tokenRateLimit.mu.Lock()
	tokenRateLimit.failures = nil
	tokenRateLimit.mu.Unlock()

	for i := 0; i < tokenRateLimit.maxRecent; i++ {
		if !allowTokenValidation() {
			t.Fatalf("attempt %d was blocked but should have been allowed", i+1)
		}
		recordTokenValidationFailure()
	}
	if allowTokenValidation() {
		t.Fatal("expected the (max+1)th attempt to be blocked")
	}
}

// TestLoadCredentials_LegacyFallback exercises the legacy path migration.
func TestLoadCredentials_LegacyFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	legacy := tmp + "/.functionfly/credentials.json"
	if err := os.MkdirAll(tmp+"/.functionfly", 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{
		"version": "1.0.0",
		"user": {"id":"u1","username":"legacy-user","provider":"dev"},
		"token": "ff_legacy_token_value_1234567890",
		"token_type": "Bearer",
		"created_at": "2025-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := loadCredentialsFromFile()
	if err != nil {
		t.Fatalf("expected legacy credentials to load, got %v", err)
	}
	if creds.User.Username != "legacy-user" {
		t.Errorf("username = %q, want legacy-user", creds.User.Username)
	}
	// After successful load, the canonical path should exist.
	if _, err := os.Stat(tmp + "/.ff/credentials.json"); err != nil {
		t.Errorf("expected credentials to be migrated to ~/.ff/credentials.json: %v", err)
	}
}

// TestLoadCredentials_NoFile covers the empty case.
func TestLoadCredentials_NoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	_, err := loadCredentialsFromFile()
	if err == nil {
		t.Fatal("expected error when no credentials file exists")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error should mention 'not logged in', got %v", err)
	}
}

// TestRunLogin_TokenFlagShortCircuitNonInteractive is a regression test for a
// bug where runLogin in non-interactive mode rejected the user with "no
// FF_TOKEN set" before checking the --token flag. The token short-circuit must
// run BEFORE the env-var fallback so that `ff login --token X --no-interactive`
// works when FF_TOKEN is unset. With a clearly-invalid token, completeManualToken
// will return a token-rejected error (not the env-var error), which is what
// this test asserts.
func TestRunLogin_TokenFlagShortCircuitNonInteractive(t *testing.T) {
	t.Setenv("FF_TOKEN", "")
	t.Setenv("FF_INVITE_CODE", "")
	// Use an obviously-invalid token so we don't depend on a live API.
	// What matters is that we get past the env-var check.
	err := runLogin("github", false, "", true, "this-token-is-long-enough-to-pass-length-check")
	if err == nil {
		t.Fatal("expected error from invalid token, got nil")
	}
	if strings.Contains(err.Error(), "not logged in and no FF_TOKEN set") {
		t.Fatalf("token flag was ignored — still got the env-var check error: %v", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected error to mention token, got %v", err)
	}
}
