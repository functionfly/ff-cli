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

var (
	compileInput   string
	compileOutput  string
	compileMode    string
	compileVerbose bool
)

// newCompilePythonCmd creates the compile python command
func newCompilePythonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "python",
		Short: "Compile Python function to WebAssembly",
		Long: `Compile a Python function to WebAssembly (WASM).

This command uses the FlyPy compiler (in-process) to transform Python
functions into deterministic WebAssembly modules that execute in the
FunctionFly runtime without requiring a Python interpreter.`,
		Example: `  # Compile a Python function
  ff compile python --input handler.py --output ./dist

  # Compile with deterministic mode
  ff compile python --input handler.py --output ./dist --mode deterministic

  # Compile with verbose output
  ff compile python -i handler.py -o ./dist -v`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompilePython(cmd)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&compileInput, "input", "i", "", "Input Python file (required)")
	cmd.Flags().StringVarP(&compileOutput, "output", "o", "./dist", "Output directory")
	cmd.Flags().StringVar(&compileMode, "mode", "deterministic", "Compilation mode: deterministic, complex, compatible")
	cmd.Flags().BoolVarP(&compileVerbose, "verbose", "v", false, "Verbose output")

	_ = cmd.MarkFlagRequired("input")

	return cmd
}

func runCompilePython(cmd *cobra.Command) error {
	if _, err := os.Stat(compileInput); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("input file not found: %s", compileInput)
		}
		return fmt.Errorf("could not stat %s: %w", compileInput, err)
	}

	validModes := map[string]bool{
		"deterministic": true,
		"complex":       true,
		"compatible":    true,
	}
	if !validModes[compileMode] {
		return fmt.Errorf("invalid mode: %s. Valid modes: deterministic, complex, compatible", compileMode)
	}

	absInput, err := filepath.Abs(compileInput)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	absInput, err = safeWritePath(absInput)
	if err != nil {
		return err
	}

	absOutput, err := filepath.Abs(compileOutput)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	absOutput, err = safeWritePath(absOutput)
	if err != nil {
		return err
	}

	source, err := os.ReadFile(absInput)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", absInput, err)
	}

	functionName := strings.TrimSuffix(filepath.Base(absInput), filepath.Ext(absInput))

	if !WantJSON() {
		fmt.Printf("Compiling Python function: %s\n", absInput)
		fmt.Printf("Output directory: %s\n", absOutput)
		fmt.Printf("Mode: %s\n", compileMode)
		fmt.Println()
	}

	mode := flypy.ExecutionMode(compileMode)
	compiler := flypy.NewCompiler(&flypy.Config{
		Mode:      mode,
		OutputDir: absOutput,
		Verbose:   compileVerbose,
	})

	result, err := compiler.Compile(context.Background(), string(source), functionName)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	if len(result.Warnings) > 0 {
		fmt.Println("⚠️  Compilation warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("   - %s\n", w)
		}
		fmt.Println()
	}

	if !WantJSON() {
		fmt.Println()
		fmt.Printf("✅ Compilation successful! Output written to: %s\n", absOutput)
	}
	return nil
}
