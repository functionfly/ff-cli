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
	compileRustInput   string
	compileRustOutput  string
	compileRustRelease bool
	compileRustVerbose bool
)

// newCompileRustCmd creates the compile rust command
func newCompileRustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rust",
		Short: "Compile Rust function to WebAssembly",
		Long: `Compile a Rust function to WebAssembly (WASM).

This command uses cargo to build your Rust function targeting the
wasm32-wasip1 WebAssembly runtime for FunctionFly.`,
		Example: `  # Compile a Rust function (debug mode)
  ff compile rust --input ./Cargo.toml --output ./dist

  # Compile with optimizations (release mode)
  ff compile rust --input ./Cargo.toml --output ./dist --release

  # Compile with verbose output
  ff compile rust -i ./Cargo.toml -o ./dist -v`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompileRust(cmd)
		},
	}

	cmd.Flags().StringVarP(&compileRustInput, "input", "i", "", "Input Cargo.toml file or project directory (required)")
	cmd.Flags().StringVarP(&compileRustOutput, "output", "o", "./dist", "Output directory for WASM file")
	cmd.Flags().BoolVar(&compileRustRelease, "release", true, "Build in release mode with optimizations")
	cmd.Flags().BoolVarP(&compileRustVerbose, "verbose", "v", false, "Verbose output")

	_ = cmd.MarkFlagRequired("input")

	return cmd
}

func runCompileRust(cmd *cobra.Command) error {
	// Validate input (Cargo.toml or directory)
	inputPath := compileRustInput
	var projectDir string

	if filepath.Base(inputPath) == "Cargo.toml" {
		projectDir = filepath.Dir(inputPath)
	} else if _, err := os.Stat(filepath.Join(inputPath, "Cargo.toml")); err == nil {
		projectDir = inputPath
	} else {
		return fmt.Errorf("input must be a Cargo.toml file or a directory containing Cargo.toml")
	}

	// Check if wasm32-wasip1 target is installed
	if err := checkWasiTarget(); err != nil {
		return err
	}

	// Create output directory
	if err := os.MkdirAll(compileRustOutput, 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get absolute paths
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	absOutput, err := filepath.Abs(compileRustOutput)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	absProjectDir, err = safeWritePath(absProjectDir)
	if err != nil {
		return err
	}
	absOutput, err = safeWritePath(absOutput)
	if err != nil {
		return err
	}

	// Print status
	fmt.Printf("Compiling Rust function: %s\n", absProjectDir)
	fmt.Printf("Output directory: %s\n", absOutput)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "release", false: "debug"}[compileRustRelease])
	fmt.Println()

	// Build the project
	target := "wasm32-wasip1"
	args := []string{"build", "--target", target}

	if compileRustRelease {
		args = append(args, "--release")
	}

	if compileRustVerbose {
		args = append(args, "-v")
	}

	// Run cargo build
	execCmd := exec.Command("cargo", args...)
	execCmd.Dir = absProjectDir
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	// Find the WASM file
	profile := "debug"
	if compileRustRelease {
		profile = "release"
	}

	wasmDir := filepath.Join(absProjectDir, "target", target, profile)
	wasmFiles, err := filepath.Glob(filepath.Join(wasmDir, "*.wasm"))
	if err != nil {
		return fmt.Errorf("failed to find WASM files: %w", err)
	}

	if len(wasmFiles) == 0 {
		return fmt.Errorf("no WASM files found in %s", wasmDir)
	}

	// Copy WASM file to output directory
	// Use the first one (usually the lib crate)
	srcWasm := wasmFiles[0]
	dstWasm := filepath.Join(absOutput, "function.wasm")

	if err := copyFile(srcWasm, dstWasm); err != nil {
		return fmt.Errorf("failed to copy WASM file: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Compilation successful!\n")
	fmt.Printf("   WASM file: %s\n", dstWasm)
	fmt.Println()
	fmt.Printf("To deploy, run:\n")
	fmt.Printf("   ff deploy --wasm %s\n", dstWasm)

	return nil
}

// checkWasiTarget checks if wasm32-wasip1 target is installed
func checkWasiTarget() error {
	checkCmd := exec.Command("rustup", "target", "list", "--installed")
	output, err := checkCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check wasm32-wasip1 target: %w", err)
	}

	if !contains(string(output), "wasm32-wasip1") {
		fmt.Println("Installing wasm32-wasip1 target...")
		installCmd := exec.Command("rustup", "target", "add", "wasm32-wasip1")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr

		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install wasm32-wasip1 target: %w\n"+
				"Run: rustup target add wasm32-wasip1", err)
		}
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	cleanSrc, err := safeReadPath(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cleanSrc)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
