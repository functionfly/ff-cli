package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Config represents the local CLI configuration
type Config struct {
	APIURL  string    `json:"api_url"`
	AppID   uuid.UUID `json:"app_id"`
	AppSlug string    `json:"app_slug"`
	Token   string    `json:"token,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}

// LoadConfig loads configuration from the specified path, supporting FF_CONFIG override
func LoadConfig(configPath string) (*Config, error) {
	// Check for FF_CONFIG environment variable override
	if envPath := os.Getenv("FF_CONFIG"); envPath != "" {
		configPath = envPath
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	// Read and parse config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves configuration to the specified path, creating directories as needed
func SaveConfig(config *Config, configPath string) error {
	// Check for FF_CONFIG environment variable override
	if envPath := os.Getenv("FF_CONFIG"); envPath != "" {
		configPath = envPath
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Update saved timestamp
	config.SavedAt = time.Now()

	// Marshal to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// EnsureConfigDir ensures the .ff directory exists
func EnsureConfigDir() error {
	return os.MkdirAll(".ff", 0750)
}
