package commands

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	FunctionID      string    `json:"function_id"`
	Version         string    `json:"version"`
	URL             string    `json:"url"`
	Hash            string    `json:"hash"`
	DeployedRegions []string  `json:"deployed_regions"`
	DeployedAt      time.Time `json:"deployed_at"`
}

func runPublish(access string, force, build, dryRun, asJSON, skipTypeCheck bool) error {
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	creds, err := LoadCredentials()
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
		publishReq := map[string]interface{}{
			"author":   creds.User.Username,
			"name":     manifest.Name,
			"version":  manifest.Version,
			"runtime":  manifest.Runtime,
			"bundle":   base64.StdEncoding.EncodeToString(bundle),
			"hash":     hash,
			"public":   isPublic,
			"manifest": manifest,
		}
		return client.Post("/v1/registry/publish", publishReq, &result)
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
