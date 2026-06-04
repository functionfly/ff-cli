package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "functionfly"
	keyringUser    = "cli-credentials"
)

// Credentials stores the authenticated user's identity and token.
type Credentials struct {
	Version      string    `json:"version"`
	User         UserInfo  `json:"user"`
	Token        string    `json:"token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// UserInfo holds the authenticated user's profile.
type UserInfo struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Provider  string `json:"provider"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".ff", "credentials.json"), nil
}

// requireAuth is a convenience wrapper that returns a CLIError-tagged
// "not logged in" error so callers can short-circuit before doing any
// manifest, network, or filesystem work. It directs the user to `ff login`.
func requireAuth() (*Credentials, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, NewCLIError(err, ExitCodeAuthError, "not logged in\n   → Run: ff login")
	}
	return creds, nil
}

// LoadCredentials reads credentials from the OS keychain, falling back to the file on disk.
func LoadCredentials() (*Credentials, error) {
	// Try OS keychain first
	data, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		var creds Credentials
		if err := json.Unmarshal([]byte(data), &creds); err == nil {
			if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
				return nil, fmt.Errorf("your session has expired\n   → Run: ff login")
			}
			return &creds, nil
		}
	}

	// Fall back to file-based storage
	return loadCredentialsFromFile()
}

// loadCredentialsFromFile reads credentials from disk.
// Tries the canonical path first (~/.ff/credentials.json), then falls back to
// the legacy path (~/.functionfly/credentials.json) used by earlier CLI
// versions. Migrates the legacy file to the canonical path on first read so
// subsequent lookups are fast.
func loadCredentialsFromFile() (*Credentials, error) {
	creds, err := loadFromPath(canonicalCredentialsPath())
	if err == nil {
		return creds, nil
	}
	if !errors.Is(err, errCredentialsMissing) {
		return nil, err
	}
	creds, legacyErr := loadFromPath(legacyCredentialsPath())
	if legacyErr != nil {
		// Neither path had credentials — preserve the user-friendly
		// "not logged in" wording for callers (and `ff whoami`).
		return nil, fmt.Errorf("not logged in\n   → Run: ff login")
	}
	// Atomically copy the legacy file into the canonical path, then
	// remove the legacy file. If the rename fails, leave both files
	// in place rather than losing the user's credentials.
	if err := copyFileAtomic(legacyCredentialsPath(), canonicalCredentialsPath(), 0o600); err == nil {
		if rmErr := os.Remove(legacyCredentialsPath()); rmErr == nil {
			fmt.Fprintf(os.Stderr, "⚠️  Migrated credentials from ~/.functionfly/credentials.json → ~/.ff/credentials.json\n")
		}
	}
	return creds, nil
}

// errCredentialsMissing is the sentinel returned when a credentials file does not exist.
var errCredentialsMissing = fmt.Errorf("credentials file missing")

// loadFromPath reads, decodes, and validates a credentials file at the given path.
func loadFromPath(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errCredentialsMissing
		}
		return nil, fmt.Errorf("could not read credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("credentials file is corrupted\n   → Run: ff login")
	}
	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return nil, fmt.Errorf("your session has expired\n   → Run: ff login")
	}
	return &creds, nil
}

func canonicalCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ff", "credentials.json")
}

func legacyCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".functionfly", "credentials.json")
}

// SaveCredentials writes credentials to the OS keychain, with a file-based fallback.
func SaveCredentials(creds *Credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("could not serialize credentials: %w", err)
	}

	// Try OS keychain first
	if err := keyring.Set(keyringService, keyringUser, string(data)); err != nil {
		// Keychain unavailable (headless Linux, CI, etc.) — fall back to file
		if err := saveCredentialsToFile(creds); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "⚠️  OS keychain unavailable — credentials saved to ~/.ff/credentials.json (chmod 600)\n")
		return nil
	}

	// Keychain write succeeded — remove any legacy file to avoid confusion
	_ = deleteCredentialsFile()
	return nil
}

// saveCredentialsToFile writes credentials to disk (legacy fallback).
func saveCredentialsToFile(creds *Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize credentials: %w", err)
	}
	return writeFileAtomic(path, data, 0o600)
}

// DeleteCredentials removes credentials from both the OS keychain and disk.
func DeleteCredentials() error {
	// Remove from keychain (ignore if not found)
	_ = keyring.Delete(keyringService, keyringUser)
	// Remove legacy file
	return deleteCredentialsFile()
}

// deleteCredentialsFile removes the credentials file from disk.
func deleteCredentialsFile() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove credentials: %w", err)
	}
	return nil
}

// IsLoggedIn returns true if valid credentials exist.
func IsLoggedIn() bool {
	_, err := LoadCredentials()
	return err == nil
}

// RefreshCredentials attempts to refresh an expired or expiring credential's token.
// If the stored credentials have a refresh_token, it calls POST /auth/refresh with
// the refresh_token and updates the stored credentials. Returns the new Credentials
// or an error if refresh fails.
func RefreshCredentials(ctx context.Context) (*Credentials, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available\n   → Run: ff login")
	}

	baseURL := resolveBaseURL()
	data := url.Values{}
	data.Set("refresh_token", creds.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/auth/refresh", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var out struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token,omitempty"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid refresh response: %w", err)
	}
	if out.Token == "" {
		return nil, fmt.Errorf("refresh response missing token")
	}

	creds.Token = out.Token
	if out.RefreshToken != "" {
		creds.RefreshToken = out.RefreshToken
	}
	creds.ExpiresAt = resolveExpiresAt(out.ExpiresAt)

	if err := SaveCredentials(creds); err != nil {
		return nil, fmt.Errorf("could not save refreshed credentials: %w", err)
	}
	return creds, nil
}

// SessionExpiresIn returns a human-readable string describing when the session expires,
// or an empty string if there are no credentials or the expiry is unset.
func SessionExpiresIn() string {
	creds, err := LoadCredentials()
	if err != nil || creds.ExpiresAt.IsZero() {
		return ""
	}
	remaining := time.Until(creds.ExpiresAt)
	if remaining <= 0 {
		return "expired"
	}
	if remaining < 24*time.Hour {
		return fmt.Sprintf("%.0f hours", remaining.Hours())
	}
	days := int(remaining.Hours() / 24)
	return fmt.Sprintf("%d days", days)
}

// resolveAuthorName returns (author, name) either from an optional positional
// "author/name" argument or by reading functionfly.jsonc + stored credentials.
// When the manifest must be loaded, auth is checked first so the user gets
// the correct next-step hint (`ff login` vs `ff init`).
func resolveAuthorName(args []string) (author, name string, err error) {
	if len(args) > 0 {
		parts := splitAuthorName(args[0])
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid argument %q — expected author/name", args[0])
		}
		return parts[0], parts[1], nil
	}
	creds, cerr := requireAuth()
	if cerr != nil {
		return "", "", cerr
	}
	manifest, merr := LoadManifest("")
	if merr != nil {
		return "", "", fmt.Errorf("no functionfly.jsonc found — run 'ff init' or pass author/name as argument")
	}
	return creds.User.Username, manifest.Name, nil
}

func splitAuthorName(s string) []string {
	for i, c := range s {
		if c == '/' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
