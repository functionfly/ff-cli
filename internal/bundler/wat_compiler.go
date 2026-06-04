package bundler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// compileWATToWasm compiles WebAssembly Text format to WebAssembly binary using wat2wasm
func compileWATToWasm(watContent string) ([]byte, error) {
	return compileWATToWasmWithOptions(watContent, false)
}

// compileWATToWasmRelocatable compiles WAT to WASM as a relocatable object file
// This is needed for linking multiple WASM modules together with wasm-ld
func compileWATToWasmRelocatable(watContent string) ([]byte, error) {
	return compileWATToWasmWithOptions(watContent, true)
}

// compileWATToWasmWithOptions compiles WAT to WASM with optional relocatable flag
func compileWATToWasmWithOptions(watContent string, relocatable bool) ([]byte, error) {
	// Create temporary files for input/output
	tempDir := os.TempDir()
	watFile := filepath.Join(tempDir, fmt.Sprintf("temp-%d.wat", os.Getpid()))
	wasmFile := filepath.Join(tempDir, fmt.Sprintf("temp-%d.wasm", os.Getpid()))

	// Clean up temp files on exit
	defer func() {
		_ = os.Remove(watFile)  //nolint:errcheck
		_ = os.Remove(wasmFile) //nolint:errcheck
	}()

	// Write WAT content to temp file
	if err := os.WriteFile(watFile, []byte(watContent), 0600); err != nil {
		return nil, fmt.Errorf("failed to write WAT file: %v", err)
	}

	// Build wat2wasm command
	args := []string{watFile, "-o", wasmFile}
	if relocatable {
		args = append(args, "-r") // Relocatable output for linking
	}

	// Compile WAT to WASM using wat2wasm
	cmd := exec.Command("wat2wasm", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("wat2wasm compilation failed: %v\nOutput: %s", err, string(output))
	}

	// Read compiled WASM bytes
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read compiled WASM file: %v", err)
	}

	return wasmBytes, nil
}
