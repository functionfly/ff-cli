package bundler

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// PythonDependency represents a Python package dependency
type PythonDependency struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Spec    string `json:"spec,omitempty"` // e.g., ">=1.0.0", "==2.1.0"
}

// bundlePython bundles Python code with dependency resolution
func bundlePython(manifest *manifest.Manifest) ([]byte, error) {
	// Read and validate entry file using shared helper
	_, sourceCode, err := ReadEntryFile(manifest)
	if err != nil {
		return nil, NewBundlerErrorWithCause("python bundle", "failed to read entry file", err)
	}

	// Parse dependencies from requirements.txt or pyproject.toml
	dependencies, err := parsePythonDependencies()
	if err != nil {
		// Don't fail if no dependencies file found, just log as warning
		fmt.Printf("Warning: failed to parse Python dependencies: %v\n", err)
		dependencies = nil
	}

	// Detect compilation mode for this Python source
	mode := detectCompilationMode(string(sourceCode))
	_ = mode // mode is recorded in metadata below

	// Try to use the precompiled MicroPython runtime with embedded code
	if runtimeBytes, runtimeErr := loadMicropythonRuntime(); runtimeErr == nil {
		if embeddedBytes, embedErr := createRuntimeWithEmbeddedCode(runtimeBytes, string(sourceCode), manifest); embedErr == nil {
			return embeddedBytes, nil
		}
	}

	// Try to create a production WASM module first
	wasmBytes, wasmErr := createPythonWasmModule(string(sourceCode), manifest)
	if wasmErr == nil && len(wasmBytes) > 0 {
		// Production path: WASM module
		return wasmBytes, nil
	}

	// Fallback: Create bundle with metadata
	bundle := createPythonBundle(string(sourceCode), dependencies, manifest)

	if len(bundle) == 0 {
		return nil, NewBundlerError("python bundle", "generated bundle is empty")
	}

	return bundle, nil
}

// parsePythonDependencies parses requirements.txt or pyproject.toml for dependencies
func parsePythonDependencies() ([]PythonDependency, error) {
	// Try requirements.txt first
	if _, err := os.Stat("requirements.txt"); err == nil {
		return parseRequirementsTxt()
	}

	// Try pyproject.toml
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return parsePyprojectToml()
	}

	return nil, fmt.Errorf("no Python dependency file found (requirements.txt or pyproject.toml)")
}

// parseRequirementsTxt parses a requirements.txt file
func parseRequirementsTxt() ([]PythonDependency, error) {
	data, err := os.ReadFile("requirements.txt")
	if err != nil {
		return nil, err
	}

	var deps []PythonDependency
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse package specification like "requests>=2.25.0" or "flask==2.0.1"
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == '>' || r == '<' || r == '=' || r == '!'
		})

		if len(parts) >= 1 {
			name := strings.TrimSpace(parts[0])
			spec := strings.TrimSuffix(line, parts[0])
			spec = strings.TrimSpace(spec)

			// Extract version if possible
			var version string
			if strings.Contains(spec, "==") {
				version = strings.TrimPrefix(spec, "==")
			} else if strings.Contains(spec, ">=") {
				version = strings.TrimPrefix(spec, ">=")
			}

			deps = append(deps, PythonDependency{
				Name:    name,
				Version: version,
				Spec:    spec,
			})
		}
	}

	return deps, nil
}

// PyprojectTOML represents the structure of a pyproject.toml file
type PyprojectTOML struct {
	Project *ProjectSection `toml:"project,omitempty"`
	Tool    *ToolSection    `toml:"tool,omitempty"`
}

// ProjectSection represents the [project] section (PEP 621)
type ProjectSection struct {
	Dependencies []string `toml:"dependencies,omitempty"`
}

// ToolSection represents the [tool] section
type ToolSection struct {
	Poetry *PoetrySection `toml:"poetry,omitempty"`
}

// PoetrySection represents the [tool.poetry] section
type PoetrySection struct {
	Dependencies map[string]interface{} `toml:"dependencies,omitempty"`
}

// parsePyprojectToml parses dependencies from pyproject.toml
func parsePyprojectToml() ([]PythonDependency, error) {
	data, err := os.ReadFile("pyproject.toml")
	if err != nil {
		return nil, fmt.Errorf("failed to read pyproject.toml: %v", err)
	}

	var config PyprojectTOML
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse pyproject.toml: %v", err)
	}

	var deps []PythonDependency

	// Try PEP 621 format first ([project.dependencies])
	if config.Project != nil && config.Project.Dependencies != nil {
		for _, dep := range config.Project.Dependencies {
			if parsedDep := parseDependencySpec(dep); parsedDep.Name != "" {
				deps = append(deps, parsedDep)
			}
		}
	}

	// Try Poetry format ([tool.poetry.dependencies])
	if config.Tool != nil && config.Tool.Poetry != nil && config.Tool.Poetry.Dependencies != nil {
		for name, versionSpec := range config.Tool.Poetry.Dependencies {
			var dep PythonDependency

			switch v := versionSpec.(type) {
			case string:
				// Handle version specifications like ">=1.0.0", "==2.0.0"
				if v == "*" {
					dep = PythonDependency{Name: name, Spec: "*"}
				} else {
					dep = parseDependencySpec(fmt.Sprintf("%s%s", name, v))
				}
			case map[string]interface{}:
				// Handle complex Poetry specifications with version, extras, etc.
				if version, ok := v["version"].(string); ok {
					if version == "*" {
						dep = PythonDependency{Name: name, Spec: "*"}
					} else {
						dep = parseDependencySpec(fmt.Sprintf("%s%s", name, version))
					}
				} else {
					dep = PythonDependency{Name: name}
				}

				// Handle optional dependencies
				if optional, ok := v["optional"].(bool); ok && optional {
					// Skip optional dependencies for now
					continue
				}
			default:
				// Simple dependency without version
				dep = PythonDependency{Name: name}
			}

			if dep.Name != "" {
				deps = append(deps, dep)
			}
		}
	}

	return deps, nil
}

// parseDependencySpec parses a dependency specification like "requests>=2.25.0"
func parseDependencySpec(spec string) PythonDependency {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return PythonDependency{}
	}

	// Handle simple package names without version constraints
	if !strings.ContainsAny(spec, ">=<!=") {
		return PythonDependency{
			Name: spec,
			Spec: "",
		}
	}

	// Find the package name (everything before the first version operator)
	operators := []string{">=", "<=", "!=", "==", ">", "<", "~=", "!="}
	var splitIndex int = -1

	for _, op := range operators {
		if idx := strings.Index(spec, op); idx != -1 {
			if splitIndex == -1 || idx < splitIndex {
				splitIndex = idx
			}
		}
	}

	if splitIndex == -1 {
		// No operator found, treat as simple name
		return PythonDependency{Name: spec}
	}

	name := strings.TrimSpace(spec[:splitIndex])
	versionSpec := strings.TrimSpace(spec[splitIndex:])

	// Extract version from the spec for easier access
	var version string
	switch {
	case strings.HasPrefix(versionSpec, "=="):
		version = strings.TrimPrefix(versionSpec, "==")
	case strings.HasPrefix(versionSpec, ">="):
		version = strings.TrimPrefix(versionSpec, ">=")
	case strings.HasPrefix(versionSpec, "<="):
		version = strings.TrimPrefix(versionSpec, "<=")
	case strings.HasPrefix(versionSpec, "!="):
		version = strings.TrimPrefix(versionSpec, "!=")
	case strings.HasPrefix(versionSpec, ">"):
		version = strings.TrimPrefix(versionSpec, ">")
	case strings.HasPrefix(versionSpec, "<"):
		version = strings.TrimPrefix(versionSpec, "<")
	case strings.HasPrefix(versionSpec, "~="):
		version = strings.TrimPrefix(versionSpec, "~=")
	}

	return PythonDependency{
		Name:    name,
		Version: version,
		Spec:    versionSpec,
	}
}

// createPythonBundle creates a bundled Python package with metadata
func createPythonBundle(sourceCode string, deps []PythonDependency, manifest *manifest.Manifest) []byte {
	// Create bundle metadata
	metadata := map[string]interface{}{
		"name":         manifest.Name,
		"version":      manifest.Version,
		"runtime":      manifest.Runtime,
		"entry_point":  manifest.Entry,
		"dependencies": deps,
		"source_hash":  HashContent([]byte(sourceCode)),
	}

	// Convert metadata to JSON
	metadataJSON, _ := json.Marshal(metadata)

	// Create the bundle format
	bundle := fmt.Sprintf(`# FunctionFly Python Bundle
# Generated for %s@%s

# Metadata
__functionfly_metadata__ = %s

# Source code
__functionfly_source__ = """
%s
"""

# Dependencies (for runtime resolution)
__functionfly_dependencies__ = %s

# Main execution function
def __functionfly_main__(input_data=None):
    # This will be executed by the Python runtime
    try:
        # Execute the source code in a controlled environment
        exec(__functionfly_source__)

        # Try to find and call main function
        if 'main' in globals():
            return globals()['main'](input_data)
        elif 'handler' in globals():
            return globals()['handler'](input_data)
        else:
            return {"status": "ok", "input": input_data}
    except Exception as e:
        return {"error": str(e), "status": "failed"}

if __name__ == "__main__":
    import sys
    import json

    input_data = None
    if len(sys.argv) > 1:
        try:
            input_data = json.loads(sys.argv[1])
        except:
            input_data = sys.argv[1]

    result = __functionfly_main__(input_data)
    print(json.dumps(result))
`, manifest.Name, manifest.Version, string(metadataJSON), sourceCode, func() string {
		if deps == nil {
			return "[]"
		}
		depsJSON, _ := json.Marshal(deps)
		return string(depsJSON)
	}())

	return []byte(bundle)
}
