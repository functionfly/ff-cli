package testing

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// Validator handles function validation
type Validator struct {
	manifest *manifest.Manifest
}

// NewValidator creates a new validator
func NewValidator(m *manifest.Manifest) *Validator {
	return &Validator{manifest: m}
}

// ValidateManifest validates the function manifest
func (v *Validator) ValidateManifest() ValidationResult {
	if v.manifest.Name == "" {
		return ValidationResult{
			Check:   "Manifest: Name",
			Passed:  false,
			Message: "function name is required",
		}
	}

	if v.manifest.Version == "" {
		return ValidationResult{
			Check:   "Manifest: Version",
			Passed:  false,
			Message: "function version is required",
		}
	}

	if v.manifest.Runtime == "" {
		return ValidationResult{
			Check:   "Manifest: Runtime",
			Passed:  false,
			Message: "runtime is required",
		}
	}

	// Check for valid runtime
	validRuntimes := []string{"node", "python", "go", "generic"}
	isValidRuntime := false
	for _, runtime := range validRuntimes {
		if v.manifest.Runtime == runtime {
			isValidRuntime = true
			break
		}
	}

	if !isValidRuntime {
		return ValidationResult{
			Check:   "Manifest: Runtime",
			Passed:  false,
			Message: fmt.Sprintf("invalid runtime '%s', must be one of: %s", v.manifest.Runtime, strings.Join(validRuntimes, ", ")),
		}
	}

	return ValidationResult{
		Check:   "Manifest",
		Passed:  true,
		Message: "manifest is valid",
	}
}

// ValidateRuntime validates runtime compatibility
func (v *Validator) ValidateRuntime() ValidationResult {
	// Check if runtime files exist
	switch v.manifest.Runtime {
	case "node":
		if !fileExists("package.json") {
			return ValidationResult{
				Check:   "Runtime: Node.js",
				Passed:  false,
				Message: "package.json not found for Node.js runtime",
			}
		}
	case "python":
		pythonFiles := []string{"requirements.txt", "pyproject.toml", "setup.py"}
		found := false
		for _, file := range pythonFiles {
			if fileExists(file) {
				found = true
				break
			}
		}
		if !found {
			return ValidationResult{
				Check:   "Runtime: Python",
				Passed:  false,
				Message: "no Python dependency file found (requirements.txt, pyproject.toml, or setup.py)",
			}
		}
	case "go":
		if !fileExists("go.mod") {
			return ValidationResult{
				Check:   "Runtime: Go",
				Passed:  false,
				Message: "go.mod not found for Go runtime",
			}
		}
	}

	return ValidationResult{
		Check:   "Runtime",
		Passed:  true,
		Message: fmt.Sprintf("runtime '%s' is compatible", v.manifest.Runtime),
	}
}

// ValidateDependencies validates function dependencies
func (v *Validator) ValidateDependencies() ValidationResult {
	switch v.manifest.Runtime {
	case "node":
		if !fileExists("package.json") {
			return ValidationResult{
				Check:   "Dependencies: Node.js",
				Passed:  false,
				Message: "package.json not found",
			}
		}
		// Could add more sophisticated dependency validation here
	case "python":
		// Basic check for Python dependencies
		pythonDepsFound := false
		pythonDepFiles := []string{"requirements.txt", "pyproject.toml", "setup.py"}
		for _, file := range pythonDepFiles {
			if fileExists(file) {
				pythonDepsFound = true
				break
			}
		}
		if !pythonDepsFound {
			return ValidationResult{
				Check:   "Dependencies: Python",
				Passed:  false,
				Message: "no dependency file found",
			}
		}
	case "go":
		if !fileExists("go.mod") {
			return ValidationResult{
				Check:   "Dependencies: Go",
				Passed:  false,
				Message: "go.mod not found",
			}
		}
	}

	return ValidationResult{
		Check:   "Dependencies",
		Passed:  true,
		Message: "dependencies are valid",
	}
}

// ValidateStrict runs additional strict validation checks
func (v *Validator) ValidateStrict() []ValidationResult {
	results := []ValidationResult{}

	// Check for sensitive files that shouldn't be included
	sensitiveFiles := []string{".env", ".env.local", ".env.production", "secrets.json", "config.json"}
	for _, file := range sensitiveFiles {
		if fileExists(file) {
			results = append(results, ValidationResult{
				Check:   "Security: Sensitive Files",
				Passed:  false,
				Message: fmt.Sprintf("potentially sensitive file '%s' found in project", file),
			})
		}
	}

	// Check function entry point exists
	entryPoint := v.getEntryPoint()
	if entryPoint != "" && !fileExists(entryPoint) {
		results = append(results, ValidationResult{
			Check:   "Entry Point",
			Passed:  false,
			Message: fmt.Sprintf("entry point '%s' not found", entryPoint),
		})
	}

	// Check for large files that might cause deployment issues
	largeFiles := findLargeFiles(".", 10*1024*1024) // 10MB
	if len(largeFiles) > 0 {
		results = append(results, ValidationResult{
			Check:   "File Size",
			Passed:  false,
			Message: fmt.Sprintf("large files found: %s", strings.Join(largeFiles, ", ")),
		})
	}

	// If no issues found, return success
	if len(results) == 0 {
		results = append(results, ValidationResult{
			Check:   "Strict Validation",
			Passed:  true,
			Message: "all strict validation checks passed",
		})
	}

	return results
}

// getEntryPoint returns the expected entry point file for the runtime
func (v *Validator) getEntryPoint() string {
	switch v.manifest.Runtime {
	case "node":
		return "index.js"
	case "python":
		return "main.py"
	case "go":
		return "main.go"
	default:
		return ""
	}
}

// fileExists checks if a file exists
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

// findLargeFiles finds files larger than the specified size
func findLargeFiles(dir string, maxSize int64) []string {
	var largeFiles []string

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip directories and hidden files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		if info.Size() > maxSize {
			largeFiles = append(largeFiles, path)
		}

		return nil
	})

	return largeFiles
}
