package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantBuildCmd() *cobra.Command {
	var outputPath string
	var skipTypeCheck bool

	cmd := &cobra.Command{
		Use:     "build",
		Short:   "Build an FCI artifact from TypeScript source",
		Long:    "Reads implant.yaml, compiles TypeScript to JavaScript, and creates an .fci artifact.",
		Example: "  ff implant build\n  ff implant build --output ./dist/my-implant.fci\n  ff implant build --skip-type-check",
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantBuild(outputPath, skipTypeCheck)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for .fci artifact (default: <name>.fci)")
	cmd.Flags().BoolVar(&skipTypeCheck, "skip-type-check", false, "Skip TypeScript type checking")

	return cmd
}

func runImplantBuild(outputPath string, skipTypeCheck bool) error {
	manifestPath := "implant.yaml"
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("implant.yaml not found in current directory\n   → Run 'ff implant init' first")
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read implant.yaml: %w", err)
	}

	manifest, err := implant.ParseManifest(manifestData)
	if err != nil {
		return fmt.Errorf("failed to parse implant.yaml: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	tsConfigPath := "tsconfig.json"
	if _, err := os.Stat(tsConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("tsconfig.json not found\n   → A TypeScript config is required to build implants")
	}

	if err := compileTypeScript(manifest, skipTypeCheck); err != nil {
		return fmt.Errorf("TypeScript compilation failed: %w", err)
	}

	payload, err := loadCompiledPayload(manifest)
	if err != nil {
		return fmt.Errorf("failed to load compiled payload: %w", err)
	}

	fciManifest := implant.FCIArtifactManifest{
		YAML:    string(manifestData),
		Slug:    manifest.ID,
		Runtime: "node22-typescript",
	}

	if manifest.Build != nil && manifest.Build.Runtime != "" {
		fciManifest.Runtime = manifest.Build.Runtime
	}

	fciData, err := implant.EncodeFCIArtifact(fciManifest, payload, nil)
	if err != nil {
		return fmt.Errorf("failed to encode FCI artifact: %w", err)
	}

	if outputPath == "" {
		outputPath = manifest.ID + ".fci"
	}

	if err := os.WriteFile(outputPath, fciData, 0600); err != nil {
		return fmt.Errorf("failed to write .fci artifact: %w", err)
	}

	hash := sha256.Sum256(fciData)
	hashHex := hex.EncodeToString(hash[:])

	fmt.Printf("\n✅ Built %s (%d bytes, SHA256: %s)\n", outputPath, len(fciData), hashHex[:12])
	fmt.Println("\nNext step:")
	fmt.Println("  ff implant sign      # Sign the manifest with your key")
	return nil
}

func compileTypeScript(manifest *implant.ImplantManifest, skipTypeCheck bool) error {
	entrypoint := manifest.Entrypoint
	if entrypoint == "" {
		entrypoint = guessEntrypoint(manifest)
	}

	if _, err := os.Stat(entrypoint); os.IsNotExist(err) {
		return fmt.Errorf("entrypoint file not found: %s", entrypoint)
	}

	args := []string{"npx", "tsc"}
	if skipTypeCheck {
		args = []string{"npx", "esbuild", entrypoint, "--bundle", "--platform=node", "--outdir=dist", "--format=esm"}
	} else {
		if err := runTypeCheck(); err != nil {
			return err
		}
		args = []string{"npx", "esbuild", entrypoint, "--bundle", "--platform=node", "--outdir=dist", "--format=esm"}
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("esbuild failed: %w", err)
	}

	return nil
}

func runTypeCheck() error {
	cmd := exec.Command("npx", "tsc", "--noEmit")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func guessEntrypoint(manifest *implant.ImplantManifest) string {
	candidates := []string{
		"index.ts",
		"src/index.ts",
		"runtime/index.ts",
		"bootstrap.ts",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return "index.ts"
}

func loadCompiledPayload(manifest *implant.ImplantManifest) ([]byte, error) {
	distDir := "dist"

	var jsFiles []string
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read dist directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".js" {
			jsFiles = append(jsFiles, filepath.Join(distDir, entry.Name()))
		}
	}

	if len(jsFiles) == 0 {
		return nil, fmt.Errorf("no compiled .js files found in dist directory")
	}

	var combined []byte
	for _, f := range jsFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", f, err)
		}
		combined = append(combined, data...)
		combined = append(combined, '\n')
	}

	return combined, nil
}
