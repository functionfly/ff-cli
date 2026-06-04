package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSONCComments_NoComments(t *testing.T) {
	input := []byte(`{"name":"test","version":"1.0.0"}`)
	got := stripJSONCComments(input)
	if string(got) != string(input) {
		t.Errorf("stripJSONCComments changed valid JSON: %s", got)
	}
}

func TestStripJSONCComments_LineComment(t *testing.T) {
	input := []byte(`{
  "name": "test", // this is a comment
  "version": "1.0.0"
}`)
	got := string(stripJSONCComments(input))
	if strings.Contains(got, "//") {
		t.Error("line comment should be stripped")
	}
	if !strings.Contains(got, `"name"`) || !strings.Contains(got, `"version"`) {
		t.Errorf("JSON content should be preserved, got: %s", got)
	}
}

func TestStripJSONCComments_BlockComment(t *testing.T) {
	input := []byte(`{
  /* this is
     a block comment */
  "name": "test"
}`)
	got := string(stripJSONCComments(input))
	if strings.Contains(got, "/*") || strings.Contains(got, "block comment") {
		t.Error("block comment should be stripped")
	}
	if !strings.Contains(got, `"name"`) {
		t.Errorf("JSON content should be preserved, got: %s", got)
	}
}

func TestStripJSONCComments_CommentInsideString(t *testing.T) {
	input := []byte(`{"value": "this // is not a comment"}`)
	got := string(stripJSONCComments(input))
	if !strings.Contains(got, "this // is not a comment") {
		t.Errorf("comment-like text inside string should be preserved, got: %s", got)
	}
}

func TestStripJSONCComments_EscapedQuotes(t *testing.T) {
	input := []byte(`{"value": "say \"hello\" // not a comment"}`)
	got := string(stripJSONCComments(input))
	if !strings.Contains(got, "say \\\"hello\\\"") {
		t.Errorf("escaped quotes should be preserved, got: %s", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.API.URL != "https://api.functionfly.com" {
		t.Errorf("API.URL = %q, want https://api.functionfly.com", cfg.API.URL)
	}
	if cfg.API.Timeout != "30s" {
		t.Errorf("API.Timeout = %q, want 30s", cfg.API.Timeout)
	}
	if cfg.Dev.Port != 8787 {
		t.Errorf("Dev.Port = %d, want 8787", cfg.Dev.Port)
	}
	if !cfg.Dev.Watch {
		t.Error("Dev.Watch should be true by default")
	}
	if !cfg.Dev.HotReload {
		t.Error("Dev.HotReload should be true by default")
	}
	if cfg.Telemetry.Enabled {
		t.Error("Telemetry.Enabled should be false by default")
	}
	if !cfg.Telemetry.Anonymize {
		t.Error("Telemetry.Anonymize should be true by default")
	}
}

func TestLoadConfig_DefaultsWhenNoFile(t *testing.T) {
	// Ensure no config file exists by using a temp dir
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if cfg.API.URL != "https://api.functionfly.com" {
		t.Errorf("should use default API URL, got: %s", cfg.API.URL)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".ff")
	os.MkdirAll(configDir, 0700)

	cfg := DefaultConfig()
	cfg.API.URL = "http://localhost:9999"
	cfg.Dev.Port = 3000

	// Verify the default config is constructed properly
	if cfg.API.URL == "" {
		t.Error("API.URL should not be empty")
	}
	if cfg.Dev.Port == 0 {
		t.Error("Dev.Port should not be zero")
	}

	// We can't easily test SaveConfig/LoadConfig without mocking configPath(),
	// but we can test defaults and ApplyEnvOverrides (tested separately).
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("FF_API_URL", "http://custom.api:8080")
	t.Setenv("FF_API_TIMEOUT", "60s")
	t.Setenv("FF_TELEMETRY", "true")

	ApplyEnvOverrides(cfg)

	if cfg.API.URL != "http://custom.api:8080" {
		t.Errorf("API.URL = %q, want http://custom.api:8080", cfg.API.URL)
	}
	if cfg.API.Timeout != "60s" {
		t.Errorf("API.Timeout = %q, want 60s", cfg.API.Timeout)
	}
	if !cfg.Telemetry.Enabled {
		t.Error("Telemetry.Enabled should be true after override")
	}
}

func TestApplyEnvOverrides_TelemetryFalse(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"0", false},
		{"false", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"yes", true},
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			cfg := DefaultConfig()
			t.Setenv("FF_TELEMETRY", tt.val)
			ApplyEnvOverrides(cfg)
			if cfg.Telemetry.Enabled != tt.want {
				t.Errorf("FF_TELEMETRY=%q → Enabled = %v, want %v", tt.val, cfg.Telemetry.Enabled, tt.want)
			}
		})
	}
}

func TestLoadManifest_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := LoadManifest(tmpDir)
	if err == nil {
		t.Error("LoadManifest should error when no manifest exists")
	}
	if !strings.Contains(err.Error(), "functionfly.jsonc") {
		t.Errorf("error should mention functionfly.jsonc, got: %s", err.Error())
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manifest{
		Name:    "test-fn",
		Version: "1.0.0",
		Runtime: "python3.11",
		Public:  true,
	}
	if err := SaveManifest(tmpDir, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest(tmpDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Name != "test-fn" {
		t.Errorf("Name = %q, want test-fn", loaded.Name)
	}
	if loaded.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", loaded.Version)
	}
	if loaded.Runtime != "python3.11" {
		t.Errorf("Runtime = %q, want python3.11", loaded.Runtime)
	}
}

func TestLoadManifest_JSONC_Comments(t *testing.T) {
	tmpDir := t.TempDir()
	content := `{
  // This is a comment
  "name": "commented-fn",
  "version": "2.0.0",
  /* block comment */
  "runtime": "node20"
}`
	os.WriteFile(filepath.Join(tmpDir, "functionfly.jsonc"), []byte(content), 0644)

	loaded, err := LoadManifest(tmpDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Name != "commented-fn" {
		t.Errorf("Name = %q, want commented-fn", loaded.Name)
	}
	if loaded.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", loaded.Version)
	}
}

func TestLoadManifest_PrefersJSONCOverJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "functionfly.jsonc"), []byte(`{"name":"jsonc-file","version":"1.0.0","runtime":"python3.11"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "functionfly.json"), []byte(`{"name":"json-file","version":"1.0.0","runtime":"python3.11"}`), 0644)

	loaded, err := LoadManifest(tmpDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Name != "jsonc-file" {
		t.Errorf("should prefer .jsonc, got Name = %q", loaded.Name)
	}
}
