package commands

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewPublishCmd() *cobra.Command {
	var access string
	var force bool
	var build bool
	var dryRun bool
	var asJSON bool
	var skipTypeCheck bool
	cmd := &cobra.Command{
		Use:     "publish",
		Short:   "Publish your function to the FunctionFly registry",
		Example: "  ff publish\n  ff publish --access private\n  ff publish --build\n  ff publish --dry-run\n  ff publish --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(access, force, build, dryRun, asJSON, skipTypeCheck)
		},
	}
	cmd.Flags().StringVar(&access, "access", "", "Access level: public or private")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&build, "build", false, "Build before publishing (runs flypy build if needed)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and bundle without publishing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output results as JSON")
	cmd.Flags().BoolVar(&skipTypeCheck, "skip-type-check", false, "Skip TypeScript type checking during publish")
	return cmd
}

type PublishResult struct {
	OK                 bool      `json:"ok"`
	Function           string    `json:"function"`
	Version            string    `json:"version"`
	URL                string    `json:"url"`
	Hash               string    `json:"hash"`
	FunctionID         string    `json:"function_id"`
	Runtime            string    `json:"runtime"`
	BundleSize         int       `json:"bundle_size"`
	DeployedRegions    []string  `json:"deployed_regions"`
	DeployedAt         time.Time `json:"deployed_at"`
	FeeCharged         bool      `json:"fee_charged"`
	FeeAmountUSD       float64   `json:"fee_amount_usd,omitempty"`
	VerificationStatus string    `json:"verification_status,omitempty"`
}

func runPublish(access string, force, build, dryRun, asJSON, skipTypeCheck bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	isPublic := manifest.Public
	if access == "public" {
		isPublic = true
	} else if access == "private" {
		isPublic = false
	}
	if build {
		if err := runBuildStep(manifest); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	err = WithSpinner("Validating manifest", func() error {
		return validateManifest(manifest)
	})
	if err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	bundle, err := bundleFunction(manifest)
	if err != nil {
		return fmt.Errorf("bundling failed: %w", err)
	}

	hash := computeHash(bundle)
	if !asJSON {
		fmt.Printf("✓ Computing hash: %s...\n", hash[:8])
	}
	if dryRun {
		if asJSON {
			printJSON(map[string]interface{}{"dry_run": true, "name": manifest.Name, "version": manifest.Version, "hash": hash, "size": len(bundle), "public": isPublic})
		} else {
			fmt.Printf("\n✅ Dry run complete\n")
			fmt.Printf("   Name:    %s\n", manifest.Name)
			fmt.Printf("   Version: %s\n", manifest.Version)
			fmt.Printf("   Hash:    %s\n", hash)
			fmt.Printf("   Size:    %d bytes\n", len(bundle))
			fmt.Printf("   Access:  %s\n", accessStr(isPublic))
			fmt.Printf("\nRun without --dry-run to publish.\n")
		}
		return nil
	}
	if !force && !YesMode && IsInteractive() && !asJSON {
		confirmed := PromptConfirm(fmt.Sprintf("Publish %s@%s (%s)?", manifest.Name, manifest.Version, accessStr(isPublic)), true)
		if !confirmed {
			fmt.Println("Publish cancelled.")
			return nil
		}
	}

	var result PublishResult
	err = WithFileProgress("Uploading to registry", int64(len(bundle)), func(updater FileProgressUpdater) error {
		updater(int64(len(bundle)), int64(len(bundle)))
		client := NewAPIClientWithToken(creds.Token)
		rawManifest, err := readRawManifestForPublish()
		if err != nil {
			return fmt.Errorf("could not read manifest: %w", err)
		}
		publishReq := map[string]interface{}{
			"author":   creds.User.Username,
			"name":     manifest.Name,
			"version":  manifest.Version,
			"manifest": json.RawMessage(rawManifest),
			"source": map[string]interface{}{
				"code":    string(bundle),
				"runtime": manifest.Runtime,
			},
		}
		return client.Post("/v1/functions/publish", publishReq, &result)
	})
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	if asJSON {
		printJSON(map[string]interface{}{"success": true, "function_id": result.FunctionID, "version": result.Version, "url": result.URL, "hash": result.Hash, "deployed_regions": result.DeployedRegions, "deployed_at": result.DeployedAt})
		return nil
	}
	fmt.Printf("\n✅ Published %s/%s@%s\n\n", creds.User.Username, manifest.Name, manifest.Version)
	fmt.Printf("Public URL:\n  %s\n\n", result.URL)
	fmt.Printf("Curl:\n  curl %s -d \"Hello World\"\n\n", result.URL)
	fmt.Printf("Stats will be available in ~30 seconds.\n  ff stats\n")
	return nil
}

func runBuildStep(manifest *Manifest) error {
	fmt.Printf("🔨 Building before publish...\n")
	if strings.HasPrefix(manifest.Runtime, "python3") {
		funcFile := "main.py"
		if _, err := os.Stat(funcFile); err != nil {
			funcFile = "handler.py"
		}
		cmd := exec.Command("flypy", "build", funcFile, "--quiet")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("flypy build failed: %w\n   → Install flypy: pip install flypy", err)
		}
	}
	return nil
}

func validateManifest(m *Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !isValidFunctionName(m.Name) {
		return fmt.Errorf("name must be lowercase letters, numbers, and hyphens only; max 63 characters; no leading or trailing hyphens")
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Runtime == "" {
		return fmt.Errorf("runtime is required")
	}
	return nil
}

func bundleFunction(manifest *Manifest) ([]byte, error) {
	candidates := []string{
		"index.js", "index.ts", "main.py", "handler.js", "handler.ts", "handler.py",
		"main.go", "handler.go", "main.rs", "lib.rs",
	}
	for _, f := range candidates {
		data, err := os.ReadFile(f)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("no function file found\n   → Expected one of: %v", candidates)
}

func computeHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func accessStr(public bool) string {
	if public {
		return "public"
	}
	return "private"
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

// readRawManifestForPublish reads the project's functionfly.jsonc (or .json)
// from the current working directory and returns the JSON-with-comments
// stripped, ready to ship to the orchestrator's /v1/functions/publish endpoint
// (which expects the manifest as a raw JSON object, not a Go struct).
func readRawManifestForPublish() ([]byte, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	candidates := []string{
		filepath.Join(dir, "functionfly.jsonc"),
		filepath.Join(dir, "functionfly.json"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return stripJSONCComments(data), nil
		}
	}
	return nil, fmt.Errorf("no manifest file found (looked for functionfly.jsonc, functionfly.json in %s)", dir)
}
