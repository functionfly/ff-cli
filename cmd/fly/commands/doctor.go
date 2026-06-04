package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/version"
)

type DiagnosticResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "warn", "error"
	Message string `json:"message"`
}

func NewDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run environment diagnostics",
		Long: `Check your FunctionFly CLI environment for common issues.

This command verifies:
  - CLI version and update availability
  - Authentication status
  - API connectivity
  - Project manifest validity
  - Required tools (flypy, node, python)
  - Network connectivity`,
		Example: "  ff doctor\n  ff doctor --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDoctor(asJSON bool) error {
	if !asJSON && !WantJSON() {
		fmt.Printf("🩺 FunctionFly Doctor\n\n")
	}

	results := []DiagnosticResult{}

	results = append(results, checkCLIVersion())
	results = append(results, checkAuth())
	results = append(results, checkAPIConnectivity())
	results = append(results, checkManifest())
	results = append(results, checkCredentials())
	results = append(results, checkConfig())
	results = append(results, checkRequiredTools())

	if asJSON || WantJSON() {
		printJSON(results)
		return nil
	}

	allOk := true
	for _, r := range results {
		icon := "✅"
		switch r.Status {
		case "warn":
			icon = "⚠️ "
			allOk = false
		case "error":
			icon = "❌"
			allOk = false
		}
		fmt.Printf("  %s %-28s %s\n", icon, r.Name, r.Message)
	}

	fmt.Println()
	if allOk {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("Some checks need attention.")
	}

	return nil
}

func checkCLIVersion() DiagnosticResult {
	return DiagnosticResult{
		Name:    "CLI Version",
		Status:  "ok",
		Message: fmt.Sprintf("ff %s (%s/%s)", version.Short(), runtime.GOOS, runtime.GOARCH),
	}
}

func checkAuth() DiagnosticResult {
	creds, err := LoadCredentials()
	if err != nil {
		return DiagnosticResult{
			Name:    "Authentication",
			Status:  "error",
			Message: "Not logged in — run: ff login",
		}
	}
	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return DiagnosticResult{
			Name:    "Authentication",
			Status:  "error",
			Message: "Session expired — run: ff login", //nolint:staticcheck
		}
	}
	expiresIn := "never"
	if !creds.ExpiresAt.IsZero() {
		d := time.Until(creds.ExpiresAt)
		if d < 24*time.Hour {
			expiresIn = fmt.Sprintf("%.0fh", d.Hours())
		} else {
			expiresIn = fmt.Sprintf("%.0fd", d.Hours()/24)
		}
	}
	return DiagnosticResult{
		Name:    "Authentication",
		Status:  "ok",
		Message: fmt.Sprintf("Logged in as %s (expires in %s)", creds.User.Username, expiresIn),
	}
}

func checkAPIConnectivity() DiagnosticResult {
	baseURL := APIURL()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return DiagnosticResult{
			Name:    "API Connectivity",
			Status:  "error",
			Message: fmt.Sprintf("Cannot reach %s — %v", baseURL, err),
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DiagnosticResult{
			Name:    "API Connectivity",
			Status:  "warn",
			Message: fmt.Sprintf("API returned HTTP %d", resp.StatusCode),
		}
	}
	return DiagnosticResult{
		Name:    "API Connectivity",
		Status:  "ok",
		Message: fmt.Sprintf("Connected to %s", baseURL),
	}
}

func checkManifest() DiagnosticResult {
	manifest, err := LoadManifest("")
	if err != nil {
		return DiagnosticResult{
			Name:    "Project Manifest",
			Status:  "warn",
			Message: "No functionfly.jsonc found in current directory",
		}
	}
	if err := validateManifest(manifest); err != nil {
		return DiagnosticResult{
			Name:    "Project Manifest",
			Status:  "error",
			Message: fmt.Sprintf("Invalid: %v", err),
		}
	}
	return DiagnosticResult{
		Name:    "Project Manifest",
		Status:  "ok",
		Message: fmt.Sprintf("%s v%s (%s)", manifest.Name, manifest.Version, manifest.Runtime),
	}
}

func checkCredentials() DiagnosticResult {
	_, err := LoadCredentials()
	if err != nil {
		return DiagnosticResult{
			Name:    "Credentials Storage",
			Status:  "error",
			Message: "No credentials found",
		}
	}
	return DiagnosticResult{
		Name:    "Credentials Storage",
		Status:  "ok",
		Message: "Credentials accessible",
	}
}

func checkConfig() DiagnosticResult {
	cfg, err := LoadConfig()
	if err != nil {
		return DiagnosticResult{
			Name:    "Config",
			Status:  "warn",
			Message: "Using default config",
		}
	}
	return DiagnosticResult{
		Name:    "Config",
		Status:  "ok",
		Message: fmt.Sprintf("API: %s", cfg.API.URL),
	}
}

func checkRequiredTools() DiagnosticResult {
	var missing []string

	if _, err := exec.LookPath("flypy"); err != nil {
		missing = append(missing, "flypy (pip install flypy)")
	}
	if _, err := exec.LookPath("node"); err != nil {
		missing = append(missing, "node")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			missing = append(missing, "python3")
		}
	}

	if len(missing) > 0 {
		if len(missing) == 3 {
			return DiagnosticResult{
				Name:    "Required Tools",
				Status:  "warn",
				Message: fmt.Sprintf("Optional tools not found: %s", strings.Join(missing, ", ")),
			}
		}
		return DiagnosticResult{
			Name:    "Required Tools",
			Status:  "warn",
			Message: fmt.Sprintf("Missing: %s", strings.Join(missing, ", ")),
		}
	}

	return DiagnosticResult{
		Name:    "Required Tools",
		Status:  "ok",
		Message: "flypy, node, python3 found",
	}
}

// formatDiagnosticJSON outputs diagnostics as JSON.
func formatDiagnosticJSON(results []DiagnosticResult) string {
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data)
}
