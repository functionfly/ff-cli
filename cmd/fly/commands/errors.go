// Package commands provides the CLI commands for FunctionFly.
package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/version"
)

// Exit codes for CLI commands.
// These follow the convention from the CLI production readiness plan.
const (
	ExitCodeSuccess         = 0   // Successful execution
	ExitCodeGeneralError    = 1   // General/unknown error
	ExitCodeAuthError       = 2   // Authentication/authorization error
	ExitCodeNetworkError    = 3   // Network connectivity error
	ExitCodeValidationError = 4   // Invalid input/validation error
	ExitCodeConfigError     = 5   // Configuration error
	ExitCodeInterrupted     = 130 // Interrupted (Ctrl+C)
)

// CLIError wraps an error with additional context and an exit code.
type CLIError struct {
	Err      error
	ExitCode int
	Message  string
}

// Error returns the error message.
func (e *CLIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *CLIError) Unwrap() error {
	return e.Err
}

// NewCLIError creates a new CLIError with the given exit code.
func NewCLIError(err error, exitCode int, message string) *CLIError {
	return &CLIError{
		Err:      err,
		ExitCode: exitCode,
		Message:  message,
	}
}

// GetExitCode determines the appropriate exit code for an error.
// It checks for specific error types and returns the corresponding exit code.
func GetExitCode(err error) int {
	if err == nil {
		return ExitCodeSuccess
	}

	// Check if it's a CLIError
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return cliErr.ExitCode
	}

	// Check for specific error patterns
	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Network errors
	if strings.Contains(errLower, "network") ||
		strings.Contains(errLower, "connection") ||
		strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "dial") ||
		strings.Contains(errLower, "no route to host") ||
		strings.Contains(errLower, "no internet") {
		return ExitCodeNetworkError
	}

	// Authentication errors
	if strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "authentication") ||
		strings.Contains(errLower, "credential") ||
		strings.Contains(errLower, "token") ||
		strings.Contains(errLower, "session") ||
		strings.Contains(errLower, "login") {
		return ExitCodeAuthError
	}

	// Validation errors
	if strings.Contains(errLower, "invalid") ||
		strings.Contains(errLower, "validation") ||
		strings.Contains(errLower, "required") ||
		strings.Contains(errLower, "malformed") {
		return ExitCodeValidationError
	}

	// Configuration errors
	if strings.Contains(errLower, "config") ||
		strings.Contains(errLower, "not found") ||
		strings.Contains(errLower, "permission") ||
		strings.Contains(errLower, "access denied") {
		return ExitCodeConfigError
	}

	// Default to general error
	return ExitCodeGeneralError
}

// ExitOnError prints the error message (if any) and exits with the appropriate code.
// This should be called at the end of main() after command execution.
func ExitOnError(err error) {
	if err == nil {
		os.Exit(ExitCodeSuccess)
	}

	code := GetExitCode(err)

	// Print error to stderr
	fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())

	os.Exit(code)
}

// Debug is a simple debug logging utility that writes to stderr.
// It only outputs when debug mode is enabled.
var Debug = func(format string, args ...interface{}) {
	if DebugMode {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}

// Verbose is a simple verbose logging utility that writes to stderr.
// It only outputs when verbose mode is enabled.
var Verbose = func(format string, args ...interface{}) {
	if VerboseMode {
		fmt.Fprintf(os.Stderr, "[VERBOSE] "+format+"\n", args...)
	}
}

// Trace is a simple trace logging utility for HTTP debugging.
// It only outputs when trace mode is enabled.
var Trace = func(format string, args ...interface{}) {
	if TraceMode {
		fmt.Fprintf(os.Stderr, "[TRACE] "+format+"\n", args...)
	}
}

// DebugMode, VerboseMode, TraceMode, YesMode, and OutputFormat are global flags set by command-line arguments.
// These are accessed by commands throughout the CLI.
var DebugMode = false
var VerboseMode = false
var TraceMode = false
var YesMode = false

// OutputFormat controls output rendering across commands.
// Values: "table" (default human-readable), "json", "csv".
var OutputFormat = "table"

// WantJSON returns true when the global --format flag is set to "json".
// Commands should use this to decide between human-readable and machine output.
func WantJSON() bool {
	return OutputFormat == "json"
}

// InitDebugFlags initializes the debug/verbose/trace flags on a cobra command.
// This should be called in the init() function of each command that needs these flags.
func InitDebugFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&DebugMode, "debug", false, "Enable full debug output")
	cmd.PersistentFlags().BoolVarP(&VerboseMode, "verbose", "v", false, "Enable verbose API calls")
	cmd.PersistentFlags().BoolVar(&TraceMode, "trace", false, "Enable HTTP trace with request/response bodies")
}

// PrintVersion prints version information to stdout.
func PrintVersion() {
	fmt.Println(version.Info())
}
