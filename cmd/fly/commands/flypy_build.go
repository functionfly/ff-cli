/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/flypy"
)

// flypyBuildCmd represents the flypy build command
var flypyBuildCmd = &cobra.Command{
	Use:   "build [file]",
	Short: "Compile Python function to deterministic Wasm artifact",
	Long: `Compiles a Python function to a deterministic WebAssembly artifact.

The function must be written using the FlyPy subset of Python that guarantees
deterministic execution. The compiler enforces restrictions to ensure the
function will produce identical outputs for identical inputs.

Examples:
  ff flypy build handler.py
  ff flypy build src/main.py --output=./build
  ff flypy build --config=custom.yaml`,
	Args: cobra.ExactArgs(1),
	Run:  flypyBuildRun,
}

// flypyBuildFlags holds flags specific to the build command
var flypyBuildFlags struct {
	name    string
	version string
	signKey string
}

func init() {
	flypyCmd.AddCommand(flypyBuildCmd)

	// Build-specific flags
	flypyBuildCmd.Flags().StringVar(&flypyBuildFlags.name, "name", "", "Function name (defaults to filename without extension)")
	flypyBuildCmd.Flags().StringVar(&flypyBuildFlags.version, "version", "1.0.0", "Function version")
	flypyBuildCmd.Flags().StringVar(&flypyBuildFlags.signKey, "sign-key", "", "Path to Ed25519 private key for signing (optional)")
}

// flypyBuildRun implements the flypy build command
func flypyBuildRun(cmd *cobra.Command, args []string) {
	filePath := args[0]

	// Validate input file
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: file '%s' not found\n", filePath)
		os.Exit(1)
	}

	// Read Python source
	source, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to read file '%s': %v\n", filePath, err)
		os.Exit(1)
	}

	// Determine function name
	functionName := flypyBuildFlags.name
	if functionName == "" {
		baseName := filepath.Base(filePath)
		functionName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}

	if flypyFlags.verbose {
		fmt.Printf("🔨 Building FlyPy function: %s\n", functionName)
		fmt.Printf("   Source file: %s\n", filePath)
		fmt.Printf("   Version: %s\n", flypyBuildFlags.version)
		fmt.Printf("   Output dir: %s\n", flypyFlags.output)
		fmt.Printf("   Mode: %s\n", flypyFlags.mode)
		fmt.Printf("\n")
	}

	// Load signing key if provided
	var signKey []byte
	if flypyBuildFlags.signKey != "" {
		keyData, err := os.ReadFile(flypyBuildFlags.signKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to read signing key '%s': %v\n", flypyBuildFlags.signKey, err)
			os.Exit(1)
		}
		signKey = keyData

		if flypyFlags.verbose {
			fmt.Printf("   Signing: enabled\n")
		}
	} else if flypyFlags.verbose {
		fmt.Printf("   Signing: disabled\n")
	}

	// Determine execution mode
	var mode flypy.ExecutionMode
	switch strings.ToLower(flypyFlags.mode) {
	case "deterministic":
		mode = flypy.DeterministicMode
	case "compatible":
		mode = flypy.CompatibleMode
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid mode '%s', must be 'deterministic' or 'compatible'\n", flypyFlags.mode)
		os.Exit(1)
	}

	// Create compiler configuration
	config := &flypy.Config{
		Mode:      mode,
		OutputDir: flypyFlags.output,
		Verbose:   flypyFlags.verbose,
		SignKey:   signKey,
	}

	// Create compiler
	compiler := flypy.NewCompiler(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create compiler: %v\n", err)
		os.Exit(1)
	}

	// Compile the function
	ctx := context.Background()
	result, err := compiler.Compile(ctx, string(source), functionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: compilation failed: %v\n", err)
		os.Exit(1)
	}

	// Report warnings
	if len(result.Warnings) > 0 {
		fmt.Printf("⚠️  Compilation warnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("   - %s\n", warning)
		}
		fmt.Printf("\n")
	}

	// Report success
	fmt.Printf("✅ FlyPy compilation successful!\n")
	fmt.Printf("   Function: %s\n", functionName)
	fmt.Printf("   Version: %s\n", flypyBuildFlags.version)
	fmt.Printf("   Output: %s\n", flypyFlags.output)

	if result.DeterminismProof != nil {
		fmt.Printf("   Determinism hash: %s\n", result.DeterminismProof.IRHash)
		if len(result.DeterminismProof.Capabilities) > 0 {
			fmt.Printf("   Capabilities: %v\n", result.DeterminismProof.Capabilities)
		}
	}

	fmt.Printf("\n")
	fmt.Printf("Next steps:\n")
	fmt.Printf("  ff flypy local     # Test locally\n")
	fmt.Printf("  ff flypy deploy    # Deploy to registry\n")
}
