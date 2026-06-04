package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// ProductionMicroPythonLinker provides production-ready linking of Python code with MicroPython runtime
// This implementation uses micropython.wasm directly - user code is loaded at runtime via mp_js_do_exec
type ProductionMicroPythonLinker struct {
	runtimePath string
	userCode    string
	manifest    *manifest.Manifest
}

// NewProductionMicroPythonLinker creates a production linker instance
func NewProductionMicroPythonLinker(userCode string, m *manifest.Manifest) *ProductionMicroPythonLinker {
	return &ProductionMicroPythonLinker{
		runtimePath: findMicropythonRuntimePath(),
		userCode:    userCode,
		manifest:    m,
	}
}

// findMicropythonRuntimePath locates the micropython.wasm file
func findMicropythonRuntimePath() string {
	// Get the directory of this source file for reliable path resolution
	_, filename, _, _ := runtime.Caller(0)
	sourceDir := filepath.Dir(filename)

	// Try multiple possible paths, including full and core variants
	paths := []string{
		// Paths relative to source file (most reliable)
		filepath.Join(sourceDir, "python", "micropython.wasm"),
		filepath.Join(sourceDir, "python", "micropython-full.wasm"),
		// Relative paths from working directory
		"internal/bundler/python/micropython.wasm",
		"internal/bundler/python/micropython-full.wasm",
		"bundler/python/micropython.wasm",
		"bundler/python/micropython-full.wasm",
		"../../internal/bundler/python/micropython.wasm",
		"../../internal/bundler/python/micropython-full.wasm",
		"./internal/bundler/python/micropython.wasm",
		"./internal/bundler/python/micropython-full.wasm",
	}
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			if info, err := os.Stat(abs); err == nil && !info.IsDir() {
				// Validate it's a real WASM file (>100KB, not a stub)
				if info.Size() > 100000 {
					return abs
				}
			}
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			// Validate it's a real WASM file (>100KB, not a stub)
			if info.Size() > 100000 {
				return p
			}
		}
	}
	return ""
}

// Link returns the micropython.wasm directly - user code is loaded at runtime via mp_js_do_exec
// This is the correct approach: micropython is an interpreter that receives code at runtime
func (l *ProductionMicroPythonLinker) Link() ([]byte, error) {
	if l.runtimePath == "" {
		return nil, fmt.Errorf("micropython runtime not found")
	}

	// Read and return micropython.wasm as-is
	// User Python code will be loaded at runtime via mp_js_do_exec
	runtimeBytes, err := os.ReadFile(l.runtimePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read micropython runtime: %v", err)
	}

	fmt.Printf("Using micropython runtime: %d bytes\n", len(runtimeBytes))
	fmt.Printf("User code will be loaded at runtime via mp_js_do_exec\n")

	return runtimeBytes, nil
}

// GetUserCode returns the user Python code that will be loaded at runtime
func (l *ProductionMicroPythonLinker) GetUserCode() string {
	return l.userCode
}

// CompileWithMicropython returns the micropython.wasm for runtime execution
// The user code is stored separately and loaded at runtime via mp_js_do_exec
func CompileWithMicropython(sourceCode string, m *manifest.Manifest) ([]byte, error) {
	fmt.Printf("Preparing MicroPython runtime for %s\n", m.Name)

	linker := NewProductionMicroPythonLinker(sourceCode, m)
	wasm, err := linker.Link()
	if err != nil {
		return nil, fmt.Errorf("MicroPython preparation failed: %v", err)
	}

	// Validate WASM
	if len(wasm) < 1000 {
		return nil, fmt.Errorf("WASM too small: %d bytes", len(wasm))
	}

	if wasm[0] != 0x00 || wasm[1] != 0x61 || wasm[2] != 0x73 || wasm[3] != 0x6D {
		return nil, fmt.Errorf("invalid WASM magic")
	}

	fmt.Printf("MicroPython runtime ready: %d bytes\n", len(wasm))
	return wasm, nil
}

// GetMicropythonRuntimePath returns the path to the micropython runtime
func GetMicropythonRuntimePath() string {
	return findMicropythonRuntimePath()
}

// IsMicropythonAvailable checks if micropython runtime is available
func IsMicropythonAvailable() bool {
	return findMicropythonRuntimePath() != ""
}
