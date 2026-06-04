package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewTestCmd() *cobra.Command {
	var input string
	var asJSON bool
	var verbose bool
	var local bool
	var localPort int
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test your function",
		Long: `Test your function against the deployed endpoint (default) or, with
--local, against a running ` + "`ff dev`" + ` server on localhost. Use --local
to test changes before publishing.`,
		Example: "  ff test\n  ff test --input \"Hello World\"\n  ff test --json\n  ff test --local\n  ff test --local --port 8080",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(input, asJSON, verbose, local, localPort)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Input to send to the function")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output results as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show request/response headers")
	cmd.Flags().BoolVar(&local, "local", false, "Test against local ff dev server instead of the deployed endpoint")
	cmd.Flags().IntVar(&localPort, "port", 8787, "Local port to use with --local (default: 8787)")
	return cmd
}

func runTest(input string, asJSON, verbose, local bool, localPort int) error {
	if local {
		return runTestLocal(input, asJSON, verbose, localPort)
	}
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	cfg, _ := LoadConfig()
	baseURL := "https://api.functionfly.com"
	if cfg != nil && cfg.API.URL != "" {
		baseURL = cfg.API.URL
	}
	url := fmt.Sprintf("%s/v1/registry/%s/%s", baseURL, creds.User.Username, manifest.Name)
	if !asJSON {
		fmt.Printf("Testing %s/%s...\n", creds.User.Username, manifest.Name)
		fmt.Printf("POST %s\n\n", url)
	}
	return doTestRequest(input, asJSON, verbose, url, "Bearer "+creds.Token, false)
}

func runTestLocal(input string, asJSON, verbose bool, port int) error {
	if port == 0 {
		port = 8787
	}
	url := fmt.Sprintf("http://localhost:%d/", port)
	if !asJSON {
		fmt.Printf("Testing local dev server...\n")
		fmt.Printf("POST %s\n\n", url)
		fmt.Println("   → Make sure 'ff dev' is running in another terminal")
	}
	return doTestRequest(input, asJSON, verbose, url, "", true)
}

func doTestRequest(input string, asJSON, verbose bool, url, authHeader string, local bool) error {
	if input == "" {
		input = `"test"`
	}
	start := time.Now()
	req, err := http.NewRequest("POST", url, strings.NewReader(input))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if local {
			return fmt.Errorf("local request failed: %w\n   → Is 'ff dev' running? (start it with: ff dev)", err)
		}
		return fmt.Errorf("request failed: %w\n   → Check your internet connection", err)
	}
	defer resp.Body.Close()
	latency := time.Since(start).Milliseconds()
	body, _ := io.ReadAll(resp.Body)
	cached := resp.Header.Get("X-Cache-Hit") == "true" || resp.Header.Get("CF-Cache-Status") == "HIT"
	region := extractRegion(resp.Header.Get("CF-Ray"))
	if asJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{"status": resp.StatusCode, "body": string(body), "latency_ms": latency, "cached": cached, "region": region, "url": url}, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	statusIcon := "✅"
	if resp.StatusCode >= 400 {
		statusIcon = "❌"
	}
	fmt.Printf("Response (%d %s):\n%s\n\n", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	fmt.Printf("latency: %dms\n", latency)
	fmt.Printf("cached:  %v\n", cached)
	if region != "" {
		fmt.Printf("region:  %s\n", region)
	}
	fmt.Println()
	if resp.StatusCode < 400 {
		fmt.Printf("%s Test passed\n", statusIcon)
	} else {
		fmt.Printf("%s Test failed (HTTP %d)\n", statusIcon, resp.StatusCode)
		return fmt.Errorf("test failed with status %d", resp.StatusCode)
	}
	return nil
}

func extractRegion(cfRay string) string {
	if cfRay == "" {
		return ""
	}
	parts := strings.Split(cfRay, "-")
	if len(parts) >= 2 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return ""
}
