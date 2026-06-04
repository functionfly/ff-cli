package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// BatchPublishResult holds the result of a single function publish in a batch
type BatchPublishResult struct {
	Dir        string `json:"dir"`
	Function   string `json:"function,omitempty"`
	Version    string `json:"version,omitempty"`
	Status     string `json:"status"` // "success", "failed", "skipped"
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	FunctionID string `json:"function_id,omitempty"`
}

// BatchPublishSummary holds the overall summary of a batch publish
type BatchPublishSummary struct {
	Total      int                  `json:"total"`
	Succeeded  int                  `json:"succeeded"`
	Failed     int                  `json:"failed"`
	Skipped    int                  `json:"skipped"`
	DurationMs int64                `json:"total_duration_ms"`
	Results    []BatchPublishResult `json:"results"`
}

// NewPublishBatchCmd creates the publish-batch command
func NewPublishBatchCmd() *cobra.Command {
	var concurrency int
	var dryRun bool
	var asJSON bool
	var conflictStrategy string
	var pattern string
	var continueOnError bool
	var authorOverride string

	cmd := &cobra.Command{
		Use:   "publish-batch [directory]",
		Short: "Publish multiple functions from a directory in parallel",
		Long: `Publish multiple functions from a directory in parallel.

Each subdirectory containing a functionfly.jsonc manifest will be published
as a separate function. Useful for bulk-publishing a library of functions.

When publishing from functions/functionfly (stdlib), author is forced to
"functionfly" so all entries appear under the official namespace.

Examples:
  ff publish-batch ./functions/functionfly
  ff publish-batch ./functions/functionfly --author functionfly
  ff publish-batch ./functions --concurrency 5
  ff publish-batch ./functions --dry-run
  ff publish-batch ./functions --conflict-strategy overwrite
  ff publish-batch ./functions --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			return runPublishBatch(dir, concurrency, dryRun, asJSON, conflictStrategy, pattern, continueOnError, authorOverride)
		},
	}

	cmd.Flags().IntVar(&concurrency, "concurrency", 3, "Number of functions to publish in parallel (max 10)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate manifests without publishing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output results as JSON")
	cmd.Flags().StringVar(&conflictStrategy, "conflict-strategy", "error", "Version conflict strategy: error, overwrite, create_new")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Glob pattern to find manifests (default: */functionfly.jsonc)")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", true, "Continue publishing remaining functions if one fails")
	cmd.Flags().StringVar(&authorOverride, "author", "", "Override author for all functions (e.g. functionfly for stdlib)")

	return cmd
}

func runPublishBatch(dir string, concurrency int, dryRun, asJSON bool, conflictStrategy, pattern string, continueOnError bool, authorOverride string) error {
	startTime := time.Now()

	if info, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return NewCLIError(err, ExitCodeValidationError, fmt.Sprintf("directory not found: %s", dir))
		}
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not read %s: %v", dir, err))
	} else if !info.IsDir() {
		return NewCLIError(fmt.Errorf("%s is not a directory", dir), ExitCodeValidationError, "")
	}

	dirs, err := findFunctionDirs(dir, pattern)
	if err != nil {
		return fmt.Errorf("failed to find function directories: %w", err)
	}

	if len(dirs) == 0 {
		if !asJSON {
			fmt.Printf("No functions found in %s\n", dir)
		}
		return NewCLIError(fmt.Errorf("no functionfly.jsonc manifests found under %s", dir), ExitCodeValidationError, "")
	}

	if !asJSON {
		fmt.Printf("Found %d function(s) to publish\n", len(dirs))
		if dryRun {
			fmt.Println("Dry run mode - no functions will be published")
		}
	}

	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 10 {
		concurrency = 10
	}

	// Ensure stdlib (functions/functionfly) is always published as author "functionfly"
	forceAuthor := authorOverride
	if forceAuthor == "" && strings.Contains(filepath.ToSlash(dir), "functions/functionfly") {
		forceAuthor = "functionfly"
	}

	var apiClient *APIClient
	if !dryRun {
		apiClient, err = NewAPIClient()
		if err != nil {
			return fmt.Errorf("failed to load credentials: %w", err)
		}
	}

	work := make(chan string, len(dirs))
	var results []BatchPublishResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	var hasError bool
	var stopMu sync.Mutex

	for _, d := range dirs {
		work <- d
	}
	close(work)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fnDir := range work {
				stopMu.Lock()
				shouldStop := hasError && !continueOnError
				stopMu.Unlock()

				if shouldStop {
					mu.Lock()
					results = append(results, BatchPublishResult{Dir: fnDir, Status: "skipped", Error: "skipped due to previous error"})
					mu.Unlock()
					continue
				}

				result := publishSingleFunction(fnDir, dryRun, conflictStrategy, apiClient, forceAuthor)

				if result.Status == "failed" {
					stopMu.Lock()
					hasError = true
					stopMu.Unlock()
				}

				mu.Lock()
				results = append(results, result)
				if !asJSON {
					printBatchResult(result)
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	summary := BatchPublishSummary{
		Total:      len(dirs),
		DurationMs: time.Since(startTime).Milliseconds(),
		Results:    results,
	}
	for _, r := range results {
		switch r.Status {
		case "success":
			summary.Succeeded++
		case "failed":
			summary.Failed++
		case "skipped":
			summary.Skipped++
		}
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	fmt.Printf("\n─────────────────────────────────────────\n")
	fmt.Printf("Batch publish complete in %dms\n", summary.DurationMs)
	fmt.Printf("  ✅ Succeeded: %d\n", summary.Succeeded)
	if summary.Failed > 0 {
		fmt.Printf("  ❌ Failed:    %d\n", summary.Failed)
	}
	if summary.Skipped > 0 {
		fmt.Printf("  ⏭️  Skipped:   %d\n", summary.Skipped)
	}
	fmt.Printf("─────────────────────────────────────────\n")

	if summary.Failed > 0 {
		return fmt.Errorf("%d function(s) failed to publish", summary.Failed)
	}
	return nil
}

func findFunctionDirs(baseDir, pattern string) ([]string, error) {
	if pattern == "" {
		pattern = "*/functionfly.jsonc"
	}
	globPattern := filepath.Join(baseDir, pattern)
	matches, err := filepath.Glob(globPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern '%s': %w", globPattern, err)
	}

	rootManifest := filepath.Join(baseDir, "functionfly.jsonc")
	if _, statErr := os.Stat(rootManifest); statErr == nil {
		found := false
		for _, m := range matches {
			if m == rootManifest {
				found = true
				break
			}
		}
		if !found {
			matches = append([]string{rootManifest}, matches...)
		}
	}

	seen := make(map[string]bool)
	var dirs []string
	for _, match := range matches {
		d := filepath.Dir(match)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs, nil
}

// BatchFunctionManifest represents a functionfly.jsonc manifest for batch publishing
type BatchFunctionManifest struct {
	Author  string `json:"author"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Runtime string `json:"runtime"`
	Public  bool   `json:"public"`
}

func loadBatchManifest(dir string) (*BatchFunctionManifest, error) {
	cleanDir, err := cleanPath(dir)
	if err != nil {
		return nil, fmt.Errorf("invalid directory: %w", err)
	}
	manifestPath := filepath.Join(cleanDir, "functionfly.jsonc")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("manifest not found at %s: %w", manifestPath, err)
	}

	cleaned := string(stripJSONCComments(data))
	var m BatchFunctionManifest
	if err := json.Unmarshal([]byte(cleaned), &m); err != nil {
		return nil, fmt.Errorf("invalid manifest JSON at %s: %w", manifestPath, err)
	}
	if m.Name == "" || m.Version == "" {
		return nil, fmt.Errorf("manifest at %s missing required fields: name, version", manifestPath)
	}
	if m.Author == "" {
		m.Author = "functionfly"
	}
	return &m, nil
}

func readRawManifest(dir string) ([]byte, error) {
	cleanDir, err := cleanPath(dir)
	if err != nil {
		return nil, fmt.Errorf("invalid directory: %w", err)
	}
	manifestPath := filepath.Join(cleanDir, "functionfly.jsonc")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	return stripJSONCComments(data), nil
}

// humanizeFunctionName turns "text-truncate" into "Text truncate".
func humanizeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.Split(strings.ReplaceAll(name, "_", "-"), "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

// injectDescriptionIfMissing adds manifest["description"] from function name when missing or empty.
func injectDescriptionIfMissing(rawManifest []byte, functionName string) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(rawManifest, &m); err != nil {
		return nil, err
	}
	if desc, ok := m["description"].(string); !ok || desc == "" {
		m["description"] = humanizeFunctionName(functionName)
	}
	return json.Marshal(m)
}

// injectAuthorIntoManifest sets manifest["author"] so the stored manifest matches the publish author.
func injectAuthorIntoManifest(rawManifest []byte, author string) ([]byte, error) {
	if author == "" {
		return rawManifest, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(rawManifest, &m); err != nil {
		return nil, err
	}
	m["author"] = author
	return json.Marshal(m)
}

func readMainSourceCode(dir string, runtime string) (string, error) {
	var mainFile string
	switch runtime {
	case "python3.11", "python3.12", "python":
		mainFile = "main.py"
	case "node18", "node20", "deno":
		mainFile = "index.js"
	default:
		for _, name := range []string{"main.py", "index.js"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				mainFile = name
				break
			}
		}
	}
	if mainFile == "" {
		return "", fmt.Errorf("could not determine main source file for runtime '%s'", runtime)
	}
	data, err := os.ReadFile(filepath.Join(dir, mainFile))
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", mainFile, err)
	}
	return string(data), nil
}

type batchPublishResponse struct {
	OK         bool   `json:"ok"`
	FunctionID string `json:"function_id"`
	Message    string `json:"message"`
}

func publishSingleFunction(dir string, dryRun bool, conflictStrategy string, client *APIClient, forceAuthor string) BatchPublishResult {
	startTime := time.Now()
	result := BatchPublishResult{Dir: dir}

	manifest, err := loadBatchManifest(dir)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to load manifest: %v", err)
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}

	author := manifest.Author
	if forceAuthor != "" {
		author = forceAuthor
	}
	result.Function = fmt.Sprintf("%s/%s", author, manifest.Name)
	result.Version = manifest.Version

	if dryRun {
		result.Status = "skipped"
		result.Error = "dry run - not published"
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}

	sourceCode, err := readMainSourceCode(dir, manifest.Runtime)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to read source: %v", err)
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}

	runtime := manifest.Runtime
	if runtime == "" {
		runtime = "python3.12"
	}

	rawManifest, err := readRawManifest(dir)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to read manifest: %v", err)
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}
	// Inject description from function name when manifest has none (so backend stores it)
	rawManifest, err = injectDescriptionIfMissing(rawManifest, manifest.Name)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to prepare manifest: %v", err)
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}
	// Ensure stored manifest has the same author as the publish request (e.g. functionfly for stdlib)
	if author != "" {
		rawManifest, err = injectAuthorIntoManifest(rawManifest, author)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("failed to set manifest author: %v", err)
			result.DurationMs = time.Since(startTime).Milliseconds()
			return result
		}
	}

	reqBody := map[string]interface{}{
		"author":   author,
		"name":     manifest.Name,
		"version":  manifest.Version,
		"manifest": json.RawMessage(rawManifest),
		"source":   map[string]interface{}{"code": sourceCode, "runtime": runtime},
	}

	var resp batchPublishResponse
	publishPath := fmt.Sprintf("/v1/functions/publish?conflict_strategy=%s", conflictStrategy)
	if err := client.Post(publishPath, reqBody, &resp); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("publish failed: %v", err)
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}

	if !resp.OK {
		result.Status = "failed"
		result.Error = resp.Message
		result.DurationMs = time.Since(startTime).Milliseconds()
		return result
	}

	result.Status = "success"
	result.FunctionID = resp.FunctionID
	result.DurationMs = time.Since(startTime).Milliseconds()
	return result
}

func printBatchResult(r BatchPublishResult) {
	switch r.Status {
	case "success":
		fmt.Printf("  ✅ %s@%s (%dms)\n", r.Function, r.Version, r.DurationMs)
	case "failed":
		fmt.Printf("  ❌ %s: %s\n", r.Dir, r.Error)
	case "skipped":
		fmt.Printf("  ⏭️  %s: %s\n", r.Dir, r.Error)
	}
}
