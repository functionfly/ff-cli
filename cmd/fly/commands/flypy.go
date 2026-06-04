/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"github.com/spf13/cobra"
)

// flypyCmd represents the flypy command
var flypyCmd = &cobra.Command{
	Use:   "flypy",
	Short: "FlyPy - Deterministic Python Compiler",
	Long: `FlyPy compiles Python functions to deterministic WebAssembly artifacts
that guarantee identical execution across different environments.

Core Commands:
  build     Compile Python function to deterministic Wasm artifact
  deploy    Deploy compiled artifact to FunctionFly registry
  local     Run function locally for testing and development

Examples:
  ff flypy build handler.py
  ff flypy deploy --registry=https://api.functionfly.com
  ff flypy local --port=8080`,
}

// flypyFlags holds global flags for flypy commands
var flypyFlags struct {
	verbose bool
	config  string
	mode    string
	output  string
}

func init() {
	flypyCmd.PersistentFlags().BoolVarP(&flypyFlags.verbose, "verbose", "v", false, "Enable verbose output")
	flypyCmd.PersistentFlags().StringVar(&flypyFlags.config, "config", "", "Path to flypy config file (default: flypy.yaml)")
	flypyCmd.PersistentFlags().StringVar(&flypyFlags.mode, "mode", "deterministic", "Execution mode: deterministic or compatible")
	flypyCmd.PersistentFlags().StringVarP(&flypyFlags.output, "output", "o", "./dist", "Output directory for compiled artifacts")
	// Subcommands are added in flypy_build.go, flypy_deploy.go, flypy_local.go
}

// FlypyCmd returns the flypy command for attachment to the live root (e.g. from main).
func FlypyCmd() *cobra.Command {
	return flypyCmd
}
