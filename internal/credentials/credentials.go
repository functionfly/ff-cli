package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials represents the stored authentication information
type Credentials struct {
	Version    string    `json:"version"`
	User       User      `json:"user"`
	Token      string    `json:"token"`
	TokenType  string    `json:"token_type"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// User represents the authenticated user information
type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Provider  string `json:"provider"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// credentialsPath returns the path to the credentials file
func credentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".ff", "credentials.json")
}

// ensureConfigDir creates the .ff directory if it doesn't exist
func ensureConfigDir() error {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".ff")
	return os.MkdirAll(configDir, 0755)
}

// Save stores credentials to the filesystem
func Save(creds *Credentials) error {
	if err := ensureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	path := credentialsPath()
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// Load retrieves credentials from the filesystem
func Load() (*Credentials, error) {
	path := credentialsPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not logged in. run 'ff login' first")
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	// Check if token is expired
	if time.Now().After(creds.ExpiresAt) {
		return nil, fmt.Errorf("authentication token expired. run 'ff login' again")
	}

	return &creds, nil
}

// Delete removes stored credentials
func Delete() error {
	path := credentialsPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credentials file: %w", err)
	}
	return nil
}

// IsLoggedIn checks if valid credentials exist
func IsLoggedIn() bool {
	_, err := Load()
	return err == nil
}