package bundler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// BundleOptions contains options for the bundler
type BundleOptions struct {
	TypeCheck     bool     // Enable TypeScript type checking
	SkipTypeCheck bool     // Skip type checking (overrides TypeCheck)
	SourceMap     bool     // Generate source maps
	Minify        bool     // Minify output
	Target        string   // Target platform
	ExternalDeps  []string // External dependencies
	TSConfig      string   // Custom tsconfig path
	StrictMode    bool     // Enforce strict TypeScript
	// npm package options
	IncludePackages bool   // Include npm packages in bundle
	PackageCache    string // Custom package cache path
	// WASM compilation options
	TargetWASM bool // Compile to WebAssembly instead of JavaScript bundle
}

// typeCheckResult contains the result of type checking
//
//nolint:unused
type typeCheckResult struct {
	Errors    []TypeError
	HasErrors bool
}

// bundleJavaScript bundles JavaScript/TypeScript code using esbuild
// It supports node18, node20, deno, and bun runtimes
func bundleJavaScript(manifest *manifest.Manifest, options *BundleOptions) ([]byte, error) {
	// Read and validate entry file using shared helper
	entryFile, sourceCode, err := ReadEntryFile(manifest)
	if err != nil {
		return nil, NewBundlerErrorWithCause("javascript bundle", "failed to read entry file", err)
	}

	// Determine if we should skip type checking
	skipTypeCheck := false
	if options != nil && options.SkipTypeCheck {
		skipTypeCheck = true
	}
	// Check manifest for skipTypeCheck
	if manifest.SkipTypeCheck != nil && *manifest.SkipTypeCheck {
		skipTypeCheck = true
	}

	// Perform type checking for TypeScript files (if enabled and not skipped)
	if !skipTypeCheck && (strings.HasSuffix(entryFile, ".ts") || strings.HasSuffix(entryFile, ".tsx")) {
		// Check if type checking is enabled (defaults to true for TypeScript)
		typeCheckEnabled := true
		if options != nil && !options.TypeCheck {
			typeCheckEnabled = false
		}
		if manifest.TypeCheck != nil {
			typeCheckEnabled = *manifest.TypeCheck
		}

		if typeCheckEnabled {
			tsconfigPath := ""
			if options != nil && options.TSConfig != "" {
				tsconfigPath = options.TSConfig
			} else if manifest.TSConfig != "" {
				tsconfigPath = manifest.TSConfig
			}

			strictMode := false
			if options != nil && options.StrictMode {
				strictMode = true
			}
			if manifest.StrictMode != nil {
				strictMode = *manifest.StrictMode
			}

			typeErrors, err := typeCheckTypeScript(entryFile, tsconfigPath, strictMode)
			if err != nil {
				return nil, NewBundlerErrorWithCause("type checking", "failed to run type checker", err)
			}
			if len(typeErrors) > 0 {
				return nil, NewTypeErrorWithDetails(typeErrors)
			}
		}
	}

	// Check if esbuild is available
	if _, err := exec.LookPath("esbuild"); err != nil {
		// Fallback to simple file reading for development
		fmt.Println("Warning: esbuild not found, using simple bundling")
		return sourceCode, nil // simpleBundle just returns the source code
	}

	// Create temporary output file with unique name to avoid conflicts
	tempOut := filepath.Join(os.TempDir(), fmt.Sprintf("functionfly-js-bundle-%d.js", os.Getpid()))
	defer os.Remove(tempOut)

	// Determine target and platform based on runtime
	target := "node18"
	platform := "node"

	switch manifest.Runtime {
	case "bun":
		target = "bun"
	case "deno":
		target = "deno"
	case "node20":
		target = "node20"
	case "node18":
		target = "node18"
	}

	// Build esbuild command with optimized settings
	args := []string{
		entryFile,
		"--bundle",
		"--minify",
		"--format=esm",
		fmt.Sprintf("--target=%s", target),
		fmt.Sprintf("--platform=%s", platform),
		fmt.Sprintf("--outfile=%s", tempOut),
		"--sourcemap", // Include sourcemaps for better debugging
	}

	// Add TypeScript support if needed
	if strings.HasSuffix(entryFile, ".ts") || strings.HasSuffix(entryFile, ".tsx") {
		args = append(args, "--loader:.ts=ts", "--loader:.tsx=tsx")
	}

	// Add JSX support for React/Preact if needed
	if strings.HasSuffix(entryFile, ".jsx") {
		args = append(args, "--loader:.jsx=jsx")
	}

	cmd := exec.Command("esbuild", args...)

	// Execute compilation
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, NewCompilationErrorWithOutput("esbuild", entryFile, string(output), err)
	}

	// Read the bundled output
	bundle, err := os.ReadFile(tempOut)
	if err != nil {
		return nil, NewBundlerErrorWithCause("javascript bundle", "failed to read bundled output", err)
	}

	if len(bundle) == 0 {
		return nil, NewBundlerError("javascript bundle", "bundled output is empty")
	}

	return bundle, nil
}

// BundleJavaScriptWASM compiles JavaScript to WASM using the available compilers
// This is the WASM production path for JavaScript/TypeScript functions
func BundleJavaScriptWASM(entryFile string, manifest *manifest.Manifest) ([]byte, error) {
	// Try Javy (QuickJS) compilation first
	if wasmBytes, err := compileJSToWasm(entryFile, manifest); err == nil {
		return wasmBytes, nil
	}

	// Fall back to JS wrapper
	return createJSWasmWrapper(entryFile, manifest)
}

// createBundleOptions creates BundleOptions from manifest
func createBundleOptions(manifest *manifest.Manifest) *BundleOptions {
	options := &BundleOptions{
		// Type checking is enabled by default for TypeScript
		TypeCheck: true,
	}

	// Set options from manifest
	if manifest.SkipTypeCheck != nil {
		options.SkipTypeCheck = *manifest.SkipTypeCheck
	}
	if manifest.TSConfig != "" {
		options.TSConfig = manifest.TSConfig
	}
	if manifest.StrictMode != nil {
		options.StrictMode = *manifest.StrictMode
	}

	// npm package options
	if manifest.IncludePackages != nil {
		options.IncludePackages = *manifest.IncludePackages
	}
	if manifest.PackageCache != "" {
		options.PackageCache = manifest.PackageCache
	}

	return options
}

// HandleNpmPackages handles npm package resolution for the bundler
// It checks for package.json, resolves dependencies, and prepares them for bundling
func HandleNpmPackages(manifest *manifest.Manifest, options *BundleOptions, functionDir string) (map[string]string, error) {
	// Check if npm package support is enabled
	includePackages := options.IncludePackages
	if manifest.IncludePackages != nil {
		includePackages = *manifest.IncludePackages
	}

	if !includePackages {
		return nil, nil
	}

	// If options is nil, create from manifest
	if options == nil {
		options = createBundleOptions(manifest)
	}

	// Resolve npm packages
	resolved, err := resolveNpmPackages(context.Background(), manifest.Dependencies, options.PackageCache)
	if err != nil {
		return nil, err
	}
	_ = resolved

	// Determine the function directory
	if functionDir == "" {
		functionDir = "."
	}

	// Look for package.json in the function directory
	pkgPath := filepath.Join(functionDir, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		// No package.json found, try manifest dependencies
		if len(manifest.Dependencies) > 0 {
			return manifest.Dependencies, nil
		}
		return nil, nil
	}

	// Parse package.json
	pkg, err := ParsePackageJSON(pkgPath)
	if err != nil {
		return nil, NewBundlerErrorWithCause("npm packages", "failed to parse package.json", err)
	}

	// Validate package.json
	validationErrors := ValidatePackageJSON(pkg)
	if len(validationErrors) > 0 {
		var errorMsgs []string
		for _, e := range validationErrors {
			errorMsgs = append(errorMsgs, e.Error())
		}
		return nil, NewBundlerError("npm packages", fmt.Sprintf("package.json validation errors: %s", strings.Join(errorMsgs, "; ")))
	}

	// Get all dependencies
	deps := pkg.GetAllDependencies()

	// Handle bundledDependencies
	if len(pkg.BundledDependencies) > 0 {
		fmt.Printf("Note: bundledDependencies will be included in the bundle: %v\n", pkg.BundledDependencies)
	}

	// Handle optionalDependencies - they won't cause failure if missing
	if pkg.OptionalDependencies != nil {
		fmt.Printf("Note: optionalDependencies will be attempted but won't fail if missing: %v\n", pkg.OptionalDependencies)
	}

	// Warn about peerDependencies
	if pkg.PeerDependencies != nil {
		fmt.Printf("Warning: peerDependencies detected - ensure host project has compatible versions: %v\n", pkg.PeerDependencies)
	}

	return deps, nil
}

// resolveNpmPackages resolves npm packages using the npm registry
func resolveNpmPackages(ctx context.Context, deps map[string]string, cacheDir string) (map[string]*NPMMetadata, error) {
	if len(deps) == 0 {
		return nil, nil
	}

	// Determine cache directory
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "functionfly-npm-cache")
	}

	// Create npm client
	client := NewNPMClient(cacheDir)

	// Resolve dependencies
	fmt.Printf("Resolving %d npm dependencies...\n", len(deps))
	metadata, err := client.BuildDependencyTree(ctx, deps)
	if err != nil {
		return nil, NewBundlerErrorWithCause("npm packages", "failed to resolve dependencies", err)
	}

	fmt.Printf("Resolved %d packages (including transitive dependencies)\n", len(metadata))

	return metadata, nil
}

// InstallNpmPackages installs npm packages to a node_modules directory
func InstallNpmPackages(functionDir string, deps map[string]string) error {
	if len(deps) == 0 {
		return nil
	}

	// Save current directory
	origDir, err := os.Getwd()
	if err != nil {
		return err
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Change to function directory
	if err := os.Chdir(functionDir); err != nil {
		return NewBundlerErrorWithCause("npm install", "failed to change to function directory", err)
	}

	// Create temporary package.json with exact dependencies
	tempPkg := &PackageJSON{
		Name:        "functionfly-temp",
		Version:     "1.0.0",
		Description: "Temporary package.json for function dependencies",
	}

	// Set dependencies based on runtime
	// For now, we'll use the deps as-is
	if deps != nil {
		tempPkg.Dependencies = deps
	}

	// Write temporary package.json
	pkgBytes, err := json.MarshalIndent(tempPkg, "", "  ")
	if err != nil {
		return NewBundlerErrorWithCause("npm install", "failed to create temp package.json", err)
	}

	// Backup existing package.json if exists
	backupPath := ""
	if _, err := os.Stat("package.json"); err == nil {
		backupPath = "package.json.functionfly.bak"
		if err := os.Rename("package.json", backupPath); err != nil {
			return NewBundlerErrorWithCause("npm install", "failed to backup package.json", err)
		}
	}

	// Write new package.json
	if err := os.WriteFile("package.json", pkgBytes, 0600); err != nil {
		// Restore backup if exists
		if backupPath != "" {
			_ = os.Rename(backupPath, "package.json")
		}
		return NewBundlerErrorWithCause("npm install", "failed to write temp package.json", err)
	}

	// Try npm install
	cmd := exec.Command("npm", "install", "--production")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Restore backup if exists
		if backupPath != "" {
			_ = os.Rename(backupPath, "package.json")
		}
		return NewBundlerError("npm install", fmt.Sprintf("npm install failed: %s", string(output)))
	}

	fmt.Println("✓ npm packages installed")

	return nil
}

// typeCheckTypeScript runs tsc --noEmit on the given file
// It returns a list of type errors or an error if tsc is not available
func typeCheckTypeScript(entryFile string, customTSConfig string, strictMode bool) ([]TypeError, error) {
	// Check if tsc is available
	if _, err := exec.LookPath("tsc"); err != nil {
		// Try using npx to run tsc
		if _, err := exec.LookPath("npx"); err != nil {
			return nil, fmt.Errorf("typescript compiler (tsc) not found: %w", err)
		}
	}

	// Find or create tsconfig
	tsconfig, err := findOrCreateTsconfig(entryFile, customTSConfig, strictMode)
	if err != nil {
		return nil, fmt.Errorf("failed to find or create tsconfig: %w", err)
	}
	defer os.Remove(tsconfig)

	// Run tsc --noEmit
	var cmd *exec.Cmd
	if _, err := exec.LookPath("tsc"); err == nil {
		cmd = exec.Command("tsc", "--noEmit", "--project", tsconfig)
	} else {
		cmd = exec.Command("npx", "tsc", "--noEmit", "--project", tsconfig)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse tsc output for type errors
		return parseTypeErrors(string(output)), nil
	}

	return nil, nil
}

// findOrCreateTsconfig finds an existing tsconfig.json or creates a default one
func findOrCreateTsconfig(entryFile string, customTSConfig string, strictMode bool) (string, error) {
	// Use custom tsconfig if provided
	if customTSConfig != "" {
		if _, err := os.Stat(customTSConfig); err == nil {
			return customTSConfig, nil
		}
	}

	// Search for tsconfig.json in the project directory
	dir := filepath.Dir(entryFile)
	for {
		tsconfig := filepath.Join(dir, "tsconfig.json")
		if _, err := os.Stat(tsconfig); err == nil {
			return tsconfig, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Create default tsconfig
	defaultConfig := createDefaultTsconfig(strictMode)
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("functionfly-tsconfig-%d.json", os.Getpid()))
	if err := os.WriteFile(tempFile, []byte(defaultConfig), 0600); err != nil {
		return "", err
	}

	return tempFile, nil
}

// createDefaultTsconfig creates a default tsconfig.json content
func createDefaultTsconfig(strictMode bool) string {
	strictOption := "true"
	if !strictMode {
		strictOption = "false"
	}

	return fmt.Sprintf(`{
	"compilerOptions": {
		"target": "ES2020",
		"module": "ESNext",
		"strict": %s,
		"esModuleInterop": true,
		"skipLibCheck": true,
		"forceConsistentCasingInFileNames": true,
		"moduleResolution": "bundler",
		"allowImportingTsExtensions": true,
		"noEmit": true,
		"lib": ["ES2020"],
		"resolveJsonModule": true,
		"isolatedModules": true
	},
	"include": ["**/*.ts", "**/*.tsx"]
}`, strictOption)
}

// parseTypeErrors parses tsc output into TypeError structs
func parseTypeErrors(output string) []TypeError {
	var errors []TypeError

	// Parse output in format: file.ts(line,col): error TS1234: message
	// Or: file.ts(line,col): error TS1234: message
	re := regexp.MustCompile(`(.+?)\((\d+),(\d+)\):\s+error\s+(TS\d+):\s+(.+)`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if len(matches) == 6 {
			errors = append(errors, TypeError{
				File:    matches[1],
				Line:    mustParseInt(matches[2]),
				Column:  mustParseInt(matches[3]),
				Code:    matches[4],
				Message: matches[5],
			})
		}
	}

	return errors
}

// mustParseInt safely parses a string to int
func mustParseInt(s string) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0
	}
	return n
}

// ParseTsconfig parses a tsconfig.json file
func ParseTsconfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config, nil
}

// GetTsconfigPaths returns the paths configuration from tsconfig
func GetTsconfigPaths(tsconfig map[string]interface{}) (map[string]string, string) {
	compilerOptions, ok := tsconfig["compilerOptions"].(map[string]interface{})
	if !ok {
		return nil, ""
	}

	paths, _ := compilerOptions["paths"].(map[string]interface{})
	baseUrl, _ := compilerOptions["baseUrl"].(string)

	// Convert interface{} map to string map
	pathMap := make(map[string]string)
	for k, v := range paths {
		if vs, ok := v.([]interface{}); ok && len(vs) > 0 {
			if s, ok := vs[0].(string); ok {
				pathMap[k] = s
			}
		}
	}

	return pathMap, baseUrl
}
