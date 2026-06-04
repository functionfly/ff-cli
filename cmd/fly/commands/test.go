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
	cmd := &cobra.Command{
		Use:     "test",
		Short:   "Test your deployed function",
		Example: "  ff test\n  ff test --input \"Hello World\"\n  ff test --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(input, asJSON, verbose)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Input to send to the function")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output results as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show request/response headers")
	return cmd
}

func runTest(input string, asJSON, verbose bool) error {
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	creds, err := LoadCredentials()
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
	if input == "" {
		input = `"test"`
	}
	start := time.Now()
	req, err := http.NewRequest("POST", url, strings.NewReader(input))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.Token)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
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
