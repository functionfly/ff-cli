package commands

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/functionfly/ff-cli/internal/version"
)

func TestCheckCLIVersion(t *testing.T) {
	result := checkCLIVersion()
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if result.Name != "CLI Version" {
		t.Errorf("Name = %q, want CLI Version", result.Name)
	}
	want := fmt.Sprintf("ff %s (%s/%s)", version.Short(), runtime.GOOS, runtime.GOARCH)
	if result.Message != want {
		t.Errorf("Message = %q, want %q", result.Message, want)
	}
}

func TestCheckRequiredTools(t *testing.T) {
	result := checkRequiredTools()
	if result.Name != "Required Tools" {
		t.Errorf("Name = %q, want Required Tools", result.Name)
	}
	// Status should be ok or warn depending on what's installed
	if result.Status != "ok" && result.Status != "warn" {
		t.Errorf("Status = %q, want ok or warn", result.Status)
	}
}

func TestCheckConfig(t *testing.T) {
	result := checkConfig()
	if result.Name != "Config" {
		t.Errorf("Name = %q, want Config", result.Name)
	}
	if result.Status != "ok" && result.Status != "warn" {
		t.Errorf("Status = %q, want ok or warn", result.Status)
	}
	if result.Status == "ok" && !strings.Contains(result.Message, "API:") {
		t.Errorf("ok message should contain API:, got: %s", result.Message)
	}
}

func TestCheckAuth_NoCredentials(t *testing.T) {
	// When not logged in, checkAuth should return error status
	result := checkAuth()
	if result.Name != "Authentication" {
		t.Errorf("Name = %q, want Authentication", result.Name)
	}
	// Either ok (if logged in) or error (if not)
	if result.Status != "ok" && result.Status != "error" {
		t.Errorf("Status = %q, want ok or error", result.Status)
	}
	if result.Status == "error" && !strings.Contains(result.Message, "login") {
		t.Errorf("error message should mention login, got: %s", result.Message)
	}
}

func TestCheckCredentials_NoCredentials(t *testing.T) {
	result := checkCredentials()
	if result.Name != "Credentials Storage" {
		t.Errorf("Name = %q, want Credentials Storage", result.Name)
	}
}

func TestDiagnosticResult_JSON(t *testing.T) {
	results := []DiagnosticResult{
		{Name: "Test", Status: "ok", Message: "all good"},
		{Name: "Broken", Status: "error", Message: "broken"},
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded []DiagnosticResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 results, got %d", len(decoded))
	}
	if decoded[0].Name != "Test" || decoded[0].Status != "ok" {
		t.Errorf("first result mismatch: %+v", decoded[0])
	}
	if decoded[1].Status != "error" {
		t.Errorf("second result status = %q, want error", decoded[1].Status)
	}
}

func TestFormatDiagnosticJSON(t *testing.T) {
	results := []DiagnosticResult{
		{Name: "A", Status: "ok", Message: "fine"},
	}
	out := formatDiagnosticJSON(results)
	if !strings.Contains(out, `"name": "A"`) {
		t.Errorf("JSON output should contain name field, got: %s", out)
	}
	if !strings.Contains(out, `"status": "ok"`) {
		t.Errorf("JSON output should contain status field, got: %s", out)
	}
}
