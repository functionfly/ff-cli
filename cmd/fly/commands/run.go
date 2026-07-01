package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/version"
)

func NewRunCmd() *cobra.Command {
	var input string
	var file string
	var method string
	var headers []string
	var timeout int
	var asJSON bool
	var raw bool
	cmd := &cobra.Command{
		Use:     "run [author/name]",
		Aliases: []string{"invoke"},
		Short:   "Execute a deployed function",
		Long: `Execute a deployed function directly from the CLI.

Sends a request to the function's endpoint and prints the response. Input can
be provided via --input (inline string), --file (read from file), or stdin
(piped). Useful for scripting, CI pipelines, and quick ad-hoc testing.`,
		Example: `  ff run alice/hello
  ff run alice/hello --input '{"name": "world"}'
  echo '{"name": "world"}' | ff run alice/hello
  ff run alice/hello --file payload.json
  ff run alice/hello --method GET
  ff run alice/hello --header "X-Custom: value" --timeout 60
  ff run alice/hello --raw | jq .result`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInvoke(args, input, file, method, headers, timeout, asJSON, raw)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Input body to send (string)")
	cmd.Flags().StringVar(&file, "file", "", "Read request body from file")
	cmd.Flags().StringVarP(&method, "method", "X", "POST", "HTTP method to use")
	cmd.Flags().StringArrayVarP(&headers, "header", "H", nil, "Extra header (repeatable, format: 'Key: Value')")
	cmd.Flags().IntVar(&timeout, "timeout", 30, "Request timeout in seconds")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output result as JSON")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print only the response body (for piping)")
	return cmd
}

func runInvoke(args []string, input, file, method string, headers []string, timeout int, asJSON, raw bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}

	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}

	body, err := resolveBody(input, file)
	if err != nil {
		return err
	}

	baseURL := APIURL()
	url := fmt.Sprintf("%s/v1/fx/%s/%s", baseURL, author, name)

	start := time.Now()
	req, err := http.NewRequest(strings.ToUpper(method), url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.Token)
	req.Header.Set("User-Agent", "ff-cli/"+version.Short())

	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			return fmt.Errorf("invalid header format %q — expected 'Key: Value'", h)
		}
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w\n   → Check your internet connection or try: ff health %s/%s", err, author, name)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	latency := time.Since(start).Milliseconds()
	cached := resp.Header.Get("X-Cache-Hit") == "true" || resp.Header.Get("CF-Cache-Status") == "HIT"
	region := extractRegion(resp.Header.Get("CF-Ray"))

	if raw {
		fmt.Print(string(respBody))
		return nil
	}

	if asJSON || WantJSON() {
		var parsed interface{}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			parsed = string(respBody)
		}
		printJSON(map[string]interface{}{
			"status":    resp.StatusCode,
			"body":      parsed,
			"latency_ms": latency,
			"cached":    cached,
			"region":    region,
		})
		return nil
	}

	statusIcon := "✅"
	if resp.StatusCode >= 400 {
		statusIcon = "❌"
	}

	fmt.Printf("%s %s %s/%s → %d %s\n\n", statusIcon, strings.ToUpper(method), author, name, resp.StatusCode, http.StatusText(resp.StatusCode))
	fmt.Printf("%s\n", string(respBody))
	fmt.Printf("\nlatency: %dms", latency)
	if cached {
		fmt.Printf("  (cached)")
	}
	if region != "" {
		fmt.Printf("  region: %s", region)
	}
	fmt.Println()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("function returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func resolveBody(input, file string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("could not read file %s: %w", file, err)
		}
		return string(data), nil
	}
	if input != "" {
		return input, nil
	}
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("could not read stdin: %w", err)
		}
		return string(data), nil
	}
	return `"test"`, nil
}
