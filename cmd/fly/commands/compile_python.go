/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
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

This command uses the FlyPy compiler to transform Python functions
into deterministic WebAssembly modules that execute in the
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
	}

	cmd.Flags().StringVarP(&compileInput, "input", "i", "", "Input Python file (required)")
	cmd.Flags().StringVarP(&compileOutput, "output", "o", "./dist", "Output directory")
	cmd.Flags().StringVar(&compileMode, "mode", "deterministic", "Compilation mode: deterministic, complex, compatible")
	cmd.Flags().BoolVarP(&compileVerbose, "verbose", "v", false, "Verbose output")

	cmd.MarkFlagRequired("input")

	return cmd
}

func runCompilePython(cmd *cobra.Command) error {
	// Validate input file exists
	if _, err := os.Stat(compileInput); os.IsNotExist(err) {
		return fmt.Errorf("input file not found: %s", compileInput)
	}

	// Validate mode
	validModes := map[string]bool{
		"deterministic": true,
		"complex":       true,
		"compatible":    true,
	}
	if !validModes[compileMode] {
		return fmt.Errorf("invalid mode: %s. Valid modes: deterministic, complex, compatible", compileMode)
	}

	// Try to find flypy-go binary in common locations
	flypyGoPaths := []string{
		"./cmd/flypy-go/flypy-go",
		"flypy-go",
	}

	var flypyGoPath string
	for _, p := range flypyGoPaths {
		if _, err := os.Stat(p); err == nil {
			flypyGoPath = p
			break
		}
	}

	// If not found, try using go run
	if flypyGoPath == "" {
		flypyGoPath = "go"
	}

	// Build arguments
	args := []string{}
	if flypyGoPath == "go" {
		args = append(args, "run", "./cmd/flypy-go")
	}
	args = append(args, "compile")

	// Add flags
	args = append(args, "--input", compileInput)
	args = append(args, "--output", compileOutput)
	args = append(args, "--mode", compileMode)
	if compileVerbose {
		args = append(args, "--verbose")
	}

	// Get absolute paths for input/output
	absInput, err := filepath.Abs(compileInput)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	absOutput, err := filepath.Abs(compileOutput)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Print status
	fmt.Printf("Compiling Python function: %s\n", absInput)
	fmt.Printf("Output directory: %s\n", absOutput)
	fmt.Printf("Mode: %s\n", compileMode)
	fmt.Println()

	// Execute flypy-go
	execCmd := exec.Command(flypyGoPath, args...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Dir = getProjectRoot()

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Compilation successful! Output written to: %s\n", absOutput)

	return nil
}

func getProjectRoot() string {
	// Get the directory where the fly binary is run from
	cwd, _ := os.Getwd()
	return cwd
}
