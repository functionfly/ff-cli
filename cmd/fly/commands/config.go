package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type GlobalConfig struct {
	Version   string    `yaml:"version" json:"version"`
	API       APIConfig `yaml:"api"     json:"api"`
	Dev       DevConfig `yaml:"dev"     json:"dev"`
	Publish   PubConfig `yaml:"publish" json:"publish"`
	Telemetry TelConfig `yaml:"telemetry" json:"telemetry"`
}

type APIConfig struct {
	URL     string `yaml:"url"     json:"url"`
	Timeout string `yaml:"timeout" json:"timeout"`
}

type DevConfig struct {
	Port      int  `yaml:"port"       json:"port"`
	Watch     bool `yaml:"watch"      json:"watch"`
	HotReload bool `yaml:"hot_reload" json:"hot_reload"`
}

type PubConfig struct {
	Confirm    bool `yaml:"confirm"     json:"confirm"`
	AutoUpdate bool `yaml:"auto_update" json:"auto_update"`
}

type TelConfig struct {
	Enabled   bool `yaml:"enabled"   json:"enabled"`
	Anonymize bool `yaml:"anonymize" json:"anonymize"`
}

func DefaultConfig() *GlobalConfig {
	return &GlobalConfig{
		Version:   "1.0.0",
		API:       APIConfig{URL: "https://api.functionfly.com", Timeout: "30s"},
		Dev:       DevConfig{Port: 8787, Watch: true, HotReload: true},
		Publish:   PubConfig{Confirm: false, AutoUpdate: true},
		Telemetry: TelConfig{Enabled: false, Anonymize: true},
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".ff", "config.yaml"), nil
}

// ConfigPath returns the path to the global config file (~/.ff/config.yaml).
// Use this when showing config location in errors or in "ff config" output.
func ConfigPath() (string, error) {
	return configPath()
}

// ApplyEnvOverrides applies FF_* environment variables over the loaded config.
// Precedence: env vars > config file > defaults. Keeps behavior consistent and doc-friendly.
func ApplyEnvOverrides(cfg *GlobalConfig) {
	if v := os.Getenv("FF_API_URL"); v != "" {
		cfg.API.URL = v
	}
	if v := os.Getenv("FF_API_TIMEOUT"); v != "" {
		cfg.API.Timeout = v
	}
	if v := os.Getenv("FF_TELEMETRY"); v != "" {
		cfg.Telemetry.Enabled = v != "0" && v != "false" && v != "no"
	}
}

func LoadConfig() (*GlobalConfig, error) {
	path, err := configPath()
	if err != nil {
		return DefaultConfig(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c := DefaultConfig()
			ApplyEnvOverrides(c)
			return c, nil
		}
		return nil, fmt.Errorf("could not read config from %s: %w\n   → Try: ff config reset or check FF_API_URL", path, err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("could not parse config at %s: %w\n   → Try: ff config reset", path, err)
	}
	ApplyEnvOverrides(cfg)
	return cfg, nil
}

func SaveConfig(cfg *GlobalConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not serialize config: %w", err)
	}
	return writeFileAtomic(path, data, 0o600)
}

type Manifest struct {
	Schema        string            `json:"$schema,omitempty"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Runtime       string            `json:"runtime"`
	Public        bool              `json:"public"`
	Deterministic bool              `json:"deterministic"`
	CacheTTL      int               `json:"cache_ttl,omitempty"`
	TimeoutMS     int               `json:"timeout_ms,omitempty"`
	MemoryMB      int               `json:"memory_mb,omitempty"`
	Description   string            `json:"description,omitempty"`
	Dependencies  map[string]string `json:"dependencies,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}

func LoadManifest(dir string) (*Manifest, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	candidates := []string{
		filepath.Join(dir, "functionfly.jsonc"),
		filepath.Join(dir, "functionfly.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		data = stripJSONCComments(data)
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("invalid manifest at %s: %w", path, err)
		}
		return &m, nil
	}
	return nil, fmt.Errorf("no functionfly.jsonc found in %s\n   → Run: ff init <name>", dir)
}

func SaveManifest(dir string, m *Manifest) error {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "functionfly.jsonc"), data, 0600)
}

func stripJSONCComments(data []byte) []byte {
	var result []byte
	inString, inLineComment, inBlockComment := false, false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inLineComment {
			if c == '\n' {
				inLineComment = false
				result = append(result, c)
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			result = append(result, c)
			if c == '\\' && i+1 < len(data) {
				i++
				result = append(result, data[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			result = append(result, c)
			continue
		}
		if c == '/' && i+1 < len(data) {
			if data[i+1] == '/' {
				inLineComment = true
				i++
				continue
			}
			if data[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}
		result = append(result, c)
	}
	return result
}

func APIURL() string {
	cfg, err := LoadConfig()
	if err != nil || cfg.API.URL == "" {
		return "https://api.functionfly.com"
	}
	return cfg.API.URL
}
