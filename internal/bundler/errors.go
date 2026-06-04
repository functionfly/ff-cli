package bundler

import (
	"fmt"
	"strings"
)

// BundlerError represents a base error type for bundler operations
//
//nolint:revive // BundlerError is kept for explicit naming consistency across the codebase
type BundlerError struct {
	Operation string
	Message   string
	Cause     error
}

func (e *BundlerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Message)
}

func (e *BundlerError) Unwrap() error {
	return e.Cause
}

// EntryFileNotFoundError is returned when no suitable entry file can be found
type EntryFileNotFoundError struct {
	Runtime      string
	Preferred    string
	Alternatives []string
}

func (e *EntryFileNotFoundError) Error() string {
	msg := fmt.Sprintf("entry file not found for runtime '%s'. Expected: %s", e.Runtime, e.Preferred)
	if len(e.Alternatives) > 0 {
		msg += fmt.Sprintf(" or alternatives: %s", strings.Join(e.Alternatives, ", "))
	}
	return msg
}

// EntryFileInvalidError is returned when the specified entry file is invalid
type EntryFileInvalidError struct {
	Path   string
	Reason string
}

func (e *EntryFileInvalidError) Error() string {
	return fmt.Sprintf("entry file '%s' is invalid: %s", e.Path, e.Reason)
}

// DependencyError represents errors related to dependency handling
type DependencyError struct {
	Operation string
	Package   string
	Message   string
	Cause     error
}

func (e *DependencyError) Error() string {
	msg := fmt.Sprintf("%s failed for package '%s': %s", e.Operation, e.Package, e.Message)
	if e.Cause != nil {
		msg += fmt.Sprintf(" (%v)", e.Cause)
	}
	return msg
}

func (e *DependencyError) Unwrap() error {
	return e.Cause
}

// CompilationError represents errors during code compilation/bundling
type CompilationError struct {
	Tool   string
	File   string
	Output string
	Cause  error
}

func (e *CompilationError) Error() string {
	msg := fmt.Sprintf("%s compilation failed", e.Tool)
	if e.File != "" {
		msg += fmt.Sprintf(" for file '%s'", e.File)
	}
	if e.Output != "" {
		msg += fmt.Sprintf(": %s", e.Output)
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(" (%v)", e.Cause)
	}
	return msg
}

func (e *CompilationError) Unwrap() error {
	return e.Cause
}

// ValidationError represents errors during template or code validation
type ValidationError struct {
	Resource string
	Issues   []ValidationIssue
}

type ValidationIssue struct {
	Path     string
	Message  string
	Severity string // "error", "warning", "info"
}

func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return fmt.Sprintf("validation failed for %s", e.Resource)
	}

	var msgs []string
	for _, issue := range e.Issues {
		msgs = append(msgs, fmt.Sprintf("[%s] %s", issue.Severity, issue.Message))
	}

	return fmt.Sprintf("validation failed for %s:\n%s", e.Resource, strings.Join(msgs, "\n"))
}

// WorkingDirectoryError represents errors related to working directory handling
type WorkingDirectoryError struct {
	Path   string
	Action string
	Cause  error
}

func (e *WorkingDirectoryError) Error() string {
	msg := fmt.Sprintf("working directory %s failed", e.Action)
	if e.Path != "" {
		msg += fmt.Sprintf(" for path '%s'", e.Path)
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *WorkingDirectoryError) Unwrap() error {
	return e.Cause
}

// RuntimeNotSupportedError is returned when an unsupported runtime is specified
type RuntimeNotSupportedError struct {
	Runtime   string
	Supported []string
}

func (e *RuntimeNotSupportedError) Error() string {
	return fmt.Sprintf("runtime '%s' is not supported. Supported runtimes: %s",
		e.Runtime, strings.Join(e.Supported, ", "))
}

// NewBundlerError creates a new BundlerError with the given operation and message
func NewBundlerError(operation, message string) *BundlerError {
	return &BundlerError{
		Operation: operation,
		Message:   message,
	}
}

// NewBundlerErrorWithCause creates a new BundlerError with a cause
func NewBundlerErrorWithCause(operation, message string, cause error) *BundlerError {
	return &BundlerError{
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

// NewDependencyError creates a new DependencyError
func NewDependencyError(operation, pkg, message string) *DependencyError {
	return &DependencyError{
		Operation: operation,
		Package:   pkg,
		Message:   message,
	}
}

// NewDependencyErrorWithCause creates a new DependencyError with a cause
func NewDependencyErrorWithCause(operation, pkg, message string, cause error) *DependencyError {
	return &DependencyError{
		Operation: operation,
		Package:   pkg,
		Message:   message,
		Cause:     cause,
	}
}

// NewCompilationError creates a new CompilationError
func NewCompilationError(tool string, cause error) *CompilationError {
	return &CompilationError{
		Tool:  tool,
		Cause: cause,
	}
}

// NewCompilationErrorWithOutput creates a new CompilationError with output
func NewCompilationErrorWithOutput(tool, file, output string, cause error) *CompilationError {
	return &CompilationError{
		Tool:   tool,
		File:   file,
		Output: output,
		Cause:  cause,
	}
}

// TypeError represents a TypeScript type checking error
type TypeError struct {
	File    string
	Line    int
	Column  int
	Message string
	Code    string // TS error code like TS2307
}

// TypeErrorInfo contains detailed type error information
type TypeErrorInfo struct {
	message string
	errors  []TypeError
}

func (e *TypeErrorInfo) Error() string {
	var sb strings.Builder
	sb.WriteString(e.message + "\n")
	for _, err := range e.errors {
		sb.WriteString(fmt.Sprintf("%s:%d:%d: error %s: %s\n",
			err.File, err.Line, err.Column, err.Code, err.Message))
	}
	return sb.String()
}

// NewTypeError creates a new type error
func NewTypeError(message string, errors []TypeError) error {
	return &TypeErrorInfo{
		message: message,
		errors:  errors,
	}
}

// NewTypeErrorWithDetails creates a new type error with details
func NewTypeErrorWithDetails(errors []TypeError) error {
	return &TypeErrorInfo{
		message: "type checking failed",
		errors:  errors,
	}
}

// GetTypeErrors returns the type errors
func (e *TypeErrorInfo) GetTypeErrors() []TypeError {
	return e.errors
}

// FormatTypeErrors formats type errors for display
func FormatTypeErrors(errors []TypeError) string {
	var sb strings.Builder
	for i, err := range errors {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%s:%d:%d: error %s: %s",
			err.File, err.Line, err.Column, err.Code, err.Message))
	}
	return sb.String()
}
