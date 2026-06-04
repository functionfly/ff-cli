package bundler

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// ReadEntryFile reads and validates the entry file specified in the manifest.
// This centralizes file reading logic and ensures consistent error handling.
func ReadEntryFile(manifest *manifest.Manifest) (string, []byte, error) {
	entryFile, err := findEntryFile(manifest)
	if err != nil {
		return "", nil, err
	}

	sourceCode, err := os.ReadFile(entryFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read entry file '%s': %v", entryFile, err)
	}

	if len(sourceCode) == 0 {
		return "", nil, fmt.Errorf("entry file '%s' is empty", entryFile)
	}

	return entryFile, sourceCode, nil
}

// ValidateEntryFile performs basic validation on an entry file without reading its contents.
func ValidateEntryFile(entryFile string) error {
	if entryFile == "" {
		return fmt.Errorf("entry file path cannot be empty")
	}

	info, err := os.Stat(entryFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("entry file '%s' not found", entryFile)
		}
		return fmt.Errorf("failed to access entry file '%s': %v", entryFile, err)
	}

	if info.IsDir() {
		return fmt.Errorf("entry file '%s' is a directory, not a file", entryFile)
	}

	if info.Size() == 0 {
		return fmt.Errorf("entry file '%s' is empty", entryFile)
	}

	return nil
}

// ResolveWorkingDirectory resolves the working directory for bundling operations.
// This ensures consistent handling of relative paths regardless of where the CLI is run from.
func ResolveWorkingDirectory(workingDir string) (string, error) {
	if workingDir == "" {
		// Use current directory if not specified
		dir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %v", err)
		}
		return dir, nil
	}

	// Convert to absolute path
	absDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory '%s': %v", workingDir, err)
	}

	// Verify the directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("working directory '%s' does not exist", absDir)
		}
		return "", fmt.Errorf("failed to access working directory '%s': %v", absDir, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("working directory '%s' is not a directory", absDir)
	}

	return absDir, nil
}

// ChangeToWorkingDirectory changes the current working directory to the specified path.
// This should be used with caution and typically restored after operations.
func ChangeToWorkingDirectory(dir string) error {
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change to working directory '%s': %v", dir, err)
	}
	return nil
}

// WithWorkingDirectory executes a function within a specific working directory,
// then restores the original working directory.
func WithWorkingDirectory(dir string, fn func() error) error {
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %v", err)
	}

	if err := ChangeToWorkingDirectory(dir); err != nil {
		return err
	}

	defer func() {
		// Always try to restore the original directory
		if restoreErr := ChangeToWorkingDirectory(originalDir); restoreErr != nil {
			// Log the error but don't override the original function error
			fmt.Fprintf(os.Stderr, "Warning: failed to restore working directory: %v\n", restoreErr)
		}
	}()

	return fn()
}
