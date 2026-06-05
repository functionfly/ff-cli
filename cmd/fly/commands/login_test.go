package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
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
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	}
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
	err := runLogin("github", false, true, "this-token-is-long-enough-to-pass-length-check")
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

// TestGetOAuthURLFromAPI_UsesGetWithQueryParams is a regression test for the
// "Invalid app slug" bug. The CLI used to POST form-encoded body to
// /auth/oauth/url, but the API only registers that path as GET — the POST
// fell through to the public function-routing handler (`/{appSlug}`) and
// came back as 400 "Invalid app slug". The fix is GET with query params.
func TestGetOAuthURLFromAPI_UsesGetWithQueryParams(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotQuery  url.Values
		gotCT     string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://example.com/oauth?x=1"}`)
	}))
	defer srv.Close()

	authURL, err := getOAuthURLFromAPI(
		srv.URL, "github",
		"http://127.0.0.1:9999/callback?state=abc",
	)
	if err != nil {
		t.Fatalf("getOAuthURLFromAPI: %v", err)
	}
	if authURL != "https://example.com/oauth?x=1" {
		t.Errorf("authURL = %q, want https://example.com/oauth?x=1", authURL)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/auth/oauth/url" {
		t.Errorf("path = %q, want /auth/oauth/url", gotPath)
	}
	if gotCT != "" {
		t.Errorf("Content-Type = %q, want empty (no form body on GET)", gotCT)
	}
	if got := gotQuery.Get("provider"); got != "github" {
		t.Errorf("provider = %q, want github", got)
	}
	if got := gotQuery.Get("redirect_uri"); got != "http://127.0.0.1:9999/callback?state=abc" {
		t.Errorf("redirect_uri = %q", got)
	}
	// Make sure we never reintroduce the old form-body fields.
	for _, banned := range []string{"app_id", "app_slug", "code_challenge", "code_challenge_method", "invite_code"} {
		if _, ok := gotQuery[banned]; ok {
			t.Errorf("query should not contain %q", banned)
		}
	}
}

// TestGetOAuthURLFromAPI_OmitsRedirectWhenEmpty covers the no-redirect sign-in
// path: when the caller doesn't supply a redirect URI the request should
// still be a well-formed GET with just the provider.
func TestGetOAuthURLFromAPI_OmitsRedirectWhenEmpty(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://example.com/oauth"}`)
	}))
	defer srv.Close()

	if _, err := getOAuthURLFromAPI(srv.URL, "google", ""); err != nil {
		t.Fatalf("getOAuthURLFromAPI: %v", err)
	}
	if got := gotQuery.Get("redirect_uri"); got != "" {
		t.Errorf("redirect_uri = %q, want empty", got)
	}
	if got := gotQuery.Get("provider"); got != "google" {
		t.Errorf("provider = %q, want google", got)
	}
}

// TestAuthServer_DeliversToken covers the happy path of the local callback
// server: a ?token=... hit resolves the WaitForCallback promise with the
// token, with no separate exchange call.
func TestAuthServer_DeliversToken(t *testing.T) {
	srv, err := newAuthServer()
	if err != nil {
		t.Fatalf("newAuthServer: %v", err)
	}
	defer srv.Close()

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", srv.Port())

	go func() {
		// Simulate the auth site redirecting the browser to the callback.
		resp, err := http.Get(callbackURL + "?token=ff_test_token_value")
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		resp.Body.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tok, err := srv.WaitForCallback(ctx)
	if err != nil {
		t.Fatalf("WaitForCallback: %v", err)
	}
	if tok != "ff_test_token_value" {
		t.Errorf("token = %q, want ff_test_token_value", tok)
	}
}

// TestAuthServer_SurfacesAuthError covers the failure path: when the auth
// site redirects with ?error=... the callback surfaces that error instead
// of silently succeeding with an empty token.
func TestAuthServer_SurfacesAuthError(t *testing.T) {
	srv, err := newAuthServer()
	if err != nil {
		t.Fatalf("newAuthServer: %v", err)
	}
	defer srv.Close()

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", srv.Port())

	go func() {
		_, _ = http.Get(callbackURL + "?error=access_denied&error_description=user+denied+the+request")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := srv.WaitForCallback(ctx); err == nil {
		t.Fatal("expected error from callback with ?error=access_denied")
	} else if !strings.Contains(err.Error(), "user denied the request") {
		t.Errorf("error should surface the auth-site error_description, got: %v", err)
	}
}
