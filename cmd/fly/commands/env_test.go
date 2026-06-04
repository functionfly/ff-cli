package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetConfigKey(t *testing.T) {
	tests := []struct {
		key, value, want string
		wantErr          bool
		setter           func(*GlobalConfig)
	}{
		{"api.url", "https://foo.com", "https://foo.com", false, func(c *GlobalConfig) { c.API.URL = "https://foo.com" }},
		{"api.timeout", "60s", "60s", false, func(c *GlobalConfig) { c.API.Timeout = "60s" }},
		{"telemetry.enabled", "true", "", false, func(c *GlobalConfig) { c.Telemetry.Enabled = true }},
		{"telemetry.enabled", "false", "", false, func(c *GlobalConfig) { c.Telemetry.Enabled = false }},
		{"telemetry.enabled", "0", "", false, func(c *GlobalConfig) { c.Telemetry.Enabled = false }},
		{"dev.port", "9000", "", false, func(c *GlobalConfig) { c.Dev.Port = 9000 }},
		{"dev.watch", "true", "", false, func(c *GlobalConfig) { c.Dev.Watch = true }},
		{"dev.watch", "false", "", false, func(c *GlobalConfig) { c.Dev.Watch = false }},
		{"dev.hot_reload", "true", "", false, func(c *GlobalConfig) { c.Dev.HotReload = true }},
		{"publish.auto_update", "true", "", false, func(c *GlobalConfig) { c.Publish.AutoUpdate = true }},
		{"invalid.key", "value", "", true, nil},
		{"dev.port", "not-a-number", "", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultConfig()
			err := setConfigKey(cfg, tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setConfigKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			wantCfg := DefaultConfig()
			tt.setter(wantCfg)
			if cfg.API.URL != wantCfg.API.URL {
				t.Errorf("API.URL = %q, want %q", cfg.API.URL, wantCfg.API.URL)
			}
			if cfg.API.Timeout != wantCfg.API.Timeout {
				t.Errorf("API.Timeout = %q, want %q", cfg.API.Timeout, wantCfg.API.Timeout)
			}
			if cfg.Telemetry.Enabled != wantCfg.Telemetry.Enabled {
				t.Errorf("Telemetry.Enabled = %v, want %v", cfg.Telemetry.Enabled, wantCfg.Telemetry.Enabled)
			}
			if cfg.Dev.Port != wantCfg.Dev.Port {
				t.Errorf("Dev.Port = %d, want %d", cfg.Dev.Port, wantCfg.Dev.Port)
			}
			if cfg.Dev.Watch != wantCfg.Dev.Watch {
				t.Errorf("Dev.Watch = %v, want %v", cfg.Dev.Watch, wantCfg.Dev.Watch)
			}
			if cfg.Dev.HotReload != wantCfg.Dev.HotReload {
				t.Errorf("Dev.HotReload = %v, want %v", cfg.Dev.HotReload, wantCfg.Dev.HotReload)
			}
			if cfg.Publish.AutoUpdate != wantCfg.Publish.AutoUpdate {
				t.Errorf("Publish.AutoUpdate = %v, want %v", cfg.Publish.AutoUpdate, wantCfg.Publish.AutoUpdate)
			}
		})
	}
}

func TestRunEnvApply_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a .env file
	envContent := `# This is a comment
export FOO=bar
BAZ="quoted value"
UNQUOTED=plain
`
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Write a manifest in the same dir so LoadManifest("") finds it
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	manifestContent := `{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Change to tmpDir so LoadManifest("") resolves correctly
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	err := runEnvApply(envPath, true)
	if err != nil {
		t.Fatalf("runEnvApply dry-run error: %v", err)
	}
}

func TestRunEnvApply_DryRun_CustomPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a .env.staging file
	envPath := filepath.Join(tmpDir, ".env.staging")
	if err := os.WriteFile(envPath, []byte("STAGE=staging\nDB_HOST=localhost\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Write a manifest in the same dir
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	err := runEnvApply(envPath, true)
	if err != nil {
		t.Fatalf("runEnvApply dry-run custom path error: %v", err)
	}
}

func TestRunEnvApply_EmptyEnvFile(t *testing.T) {
	tmpDir := t.TempDir()

	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte("# just a comment\n   \n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Dry-run with empty .env should not error
	err := runEnvApply(envPath, true)
	if err != nil {
		t.Fatalf("runEnvApply with empty env error: %v", err)
	}
}

func TestRunEnvApply_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := runEnvApply(filepath.Join(tmpDir, "nonexistent.env"), false)
	if err == nil {
		t.Error("runEnvApply should error when file not found")
	}
	if !strings.Contains(err.Error(), "env file not found") {
		t.Errorf("error should mention 'env file not found', got: %v", err)
	}
}

func TestRunEnvSet_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Change to tmp dir so manifest resolves
	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	// Dry-run should not error even without credentials
	err := runEnvSet([]string{"FOO=bar", "BAZ=qux"}, true)
	if err != nil {
		t.Fatalf("runEnvSet dry-run error: %v", err)
	}
}

func TestRunEnvSet_DryRun_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	// Invalid format should error even in dry-run
	err := runEnvSet([]string{"INVALID_NO_EQUALS"}, true)
	if err == nil {
		t.Error("runEnvSet should error on invalid format")
	}
}

func TestRunEnvUnset_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	// Dry-run should not error
	err := runEnvUnset([]string{"FOO", "BAR"}, true)
	if err != nil {
		t.Fatalf("runEnvUnset dry-run error: %v", err)
	}
}

func TestRunEnvSet_DryRun_EmptyKey(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "functionfly.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-fn","version":"1.0.0","runtime":"python3.11"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldCwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldCwd)

	err := runEnvSet([]string{"=value"}, true)
	if err == nil {
		t.Error("runEnvSet should error on empty key")
	}
}
