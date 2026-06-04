package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/functionfly/ff-cli/internal/flypy/backend"
	"github.com/functionfly/ff-cli/internal/flypy/ir"
	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// CompilePython compiles Python source code to WebAssembly.
// This is the main entry point for Python-to-WASM compilation.
func CompilePython(pythonSource string, mode string) ([]byte, error) {
	// Step 1: Parse Python to AST (30-second timeout for parsing)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pythonAST, err := parser.ParsePython(ctx, pythonSource)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Python: %w", err)
	}

	// Step 2: Generate IR from AST
	irModule, err := ir.Generate(pythonAST, "function")
	if err != nil {
		return nil, fmt.Errorf("failed to generate IR: %w", err)
	}

	// Step 3: Generate Rust code from IR with the appropriate mode
	rustCode, err := backend.GenerateRustWithMode(irModule, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Rust: %w", err)
	}

	// Step 4: Compile Rust to WASM
	return CompileRustWithMode(rustCode, "wasm32-wasip1", mode)
}

// generateCargoToml creates a Cargo.toml with dependencies based on compilation mode
func generateCargoToml(mode string) string {
	baseToml := `
[package]
name = "flypy_function"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
regex = "1"
`

	// Add extra dependencies for complex mode
	if mode == "complex" || mode == "compatible" {
		baseToml += `csv = "1.3"
sha2 = "0.10"
md5 = "0.7"
base64 = "0.22"
chrono = { version = "0.4", features = ["serde"] }
uuid = { version = "1.0", features = ["v5"] }
encoding_rs = "0.8"
`
	}

	baseToml += `
[profile.release]
opt-level = "s"
lto = true
panic = "abort"
`
	return baseToml
}

// CompileRust compiles Rust source code to Wasm using WASI target.
func CompileRust(source string, target string) ([]byte, error) {
	return CompileRustWithMode(source, target, "deterministic")
}

// CompileRustWithMode compiles Rust source code to Wasm with specified mode.
// It uses context.Background() internally; use CompileRustWithModeCtx for
// cancellation/timeout support.
func CompileRustWithMode(source string, target string, mode string) ([]byte, error) {
	return CompileRustWithModeCtx(context.Background(), source, target, mode)
}

// CompileRustWithModeCtx compiles Rust source code to Wasm with context support.
// The context is propagated to the cargo subprocess so cancellation and timeouts
// are properly honored, preventing orphaned build processes.
func CompileRustWithModeCtx(ctx context.Context, source string, target string, mode string) ([]byte, error) {
	// Create a temporary directory for the Rust project
	tempDir, err := os.MkdirTemp("", "flypy-rust-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Cargo.toml with dependencies based on mode
	cargoToml := generateCargoToml(mode)
	if err := os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte(cargoToml), 0644); err != nil {
		return nil, fmt.Errorf("failed to write Cargo.toml: %w", err)
	}

	// Create src directory
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create src directory: %w", err)
	}

	// Write lib.rs
	if err := os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write lib.rs: %w", err)
	}

	// Use WASI target for compilation (wasm32-wasip1)
	wasiTarget := "wasm32-wasip1"
	wasmBytes, err := compileWithCargoWASI(ctx, tempDir, wasiTarget)
	if err == nil {
		// Validate exports after successful compilation
		if err := ValidateEntryPoints(wasmBytes); err != nil {
			return nil, fmt.Errorf("WASM validation failed: %w", err)
		}
		return wasmBytes, nil
	}

	// Fallback: try standard wasm32-unknown-unknown target
	wasmBytes, err = compileWithCargo(ctx, tempDir, "wasm32-unknown-unknown")
	if err == nil {
		// Validate exports after successful compilation
		if err := ValidateEntryPoints(wasmBytes); err != nil {
			return nil, fmt.Errorf("WASM validation failed: %w", err)
		}
		return wasmBytes, nil
	}

	// No fallback - compilation must succeed
	// Note: we do NOT include the cargo output in the error to avoid leaking
	// generated source code into logs.
	return nil, fmt.Errorf("failed to compile WASM module: both WASI and standard compilation failed")
}

func compileWithCargoWASI(ctx context.Context, tempDir string, target string) ([]byte, error) {
	// Use CommandContext so the process is killed if ctx is cancelled/timed out
	cmd := exec.CommandContext(ctx, "cargo", "build", "--release", "--target", target)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "RUSTFLAGS=-C target-feature=-crt-static")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Do not include output (which may contain generated source) in the error message
		_ = output
		return nil, fmt.Errorf("cargo build for WASI failed: %w", err)
	}

	// Find the Wasm file in WASI target directory
	wasmPath := filepath.Join(tempDir, "target", target, "release", "flypy_function.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		// Try with .wasm extension in different location
		wasmPath = filepath.Join(tempDir, "target", target, "release", "deps", "flypy_function.wasm")
		if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Wasm file not found after WASI build")
		}
	}

	return os.ReadFile(wasmPath)
}

func compileWithCargo(ctx context.Context, tempDir string, target string) ([]byte, error) {
	// Use CommandContext so the process is killed if ctx is cancelled/timed out
	var cmd *exec.Cmd
	if target != "" {
		cmd = exec.CommandContext(ctx, "cargo", "build", "--release", "--target", target)
	} else {
		cmd = exec.CommandContext(ctx, "cargo", "build", "--release")
	}

	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Do not include output (which may contain generated source) in the error message
		_ = output
		return nil, fmt.Errorf("cargo build failed: %w", err)
	}

	// Find the Wasm file
	wasmPath := filepath.Join(tempDir, "target", target, "release", "flypy_function.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Wasm file not found after standard build")
	}

	return os.ReadFile(wasmPath)
}

// ValidateWasm checks if the given bytes represent a valid Wasm module
func ValidateWasm(wasm []byte) error {
	if len(wasm) < 8 {
		return fmt.Errorf("Wasm module too short")
	}

	// Check magic number
	magic := []byte{0x00, 0x61, 0x73, 0x6d}
	if !bytes.Equal(wasm[:4], magic) {
		return fmt.Errorf("invalid Wasm magic number")
	}

	// Check version
	version := []byte{0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(wasm[4:8], version) {
		return fmt.Errorf("invalid Wasm version")
	}

	return nil
}

// ValidateExports checks if the WASM module has valid entry point exports
// Required exports: _start, main, or handler
func ValidateExports(wasm []byte) ([]string, error) {
	if err := ValidateWasm(wasm); err != nil {
		return nil, err
	}

	var exports []string
	offset := 8 // Skip magic + version

	for offset < len(wasm) {
		if offset+2 > len(wasm) {
			break
		}

		sectionID := wasm[offset]
		offset++

		// Read section size (varint)
		sectionSize := readVarint(wasm, &offset)

		if offset+sectionSize > len(wasm) {
			break
		}

		// Export section (ID = 7)
		if sectionID == 7 {
			exports = parseExportSection(wasm[offset : offset+sectionSize])
			break
		}

		offset += sectionSize
	}

	return exports, nil
}

// ValidateEntryPoints checks if the WASM module has a valid entry point
func ValidateEntryPoints(wasm []byte) error {
	exports, err := ValidateExports(wasm)
	if err != nil {
		return err
	}

	// Check for required entry points
	hasEntryPoint := false
	for _, exp := range exports {
		if exp == "_start" || exp == "main" || exp == "handler" {
			hasEntryPoint = true
			break
		}
	}

	if !hasEntryPoint {
		return fmt.Errorf("WASM module missing entry point exports (_start, main, or handler). Found exports: %v", exports)
	}

	return nil
}

// parseExportSection parses the export section of a WASM module
func parseExportSection(data []byte) []string {
	var exports []string
	offset := 0

	if offset+4 > len(data) {
		return exports
	}

	// Number of exports (varint)
	count := readVarint(data, &offset)

	for i := 0; i < count; i++ {
		if offset >= len(data) {
			break
		}

		// Read name length
		nameLen := readVarint(data, &offset)
		if offset+nameLen > len(data) {
			break
		}

		// Read name
		name := string(data[offset : offset+nameLen])
		exports = append(exports, name)
		offset += nameLen

		// Read export descriptor (1 byte for func/table/memory/global)
		if offset >= len(data) {
			break
		}
		offset++ // skip kind

		// Read index (varint)
		if offset < len(data) {
			_ = readVarint(data, &offset)
		}
	}

	return exports
}

// readVarint reads a unsigned varint from the WASM binary
func readVarint(data []byte, offset *int) int {
	var result int
	var shift uint

	for *offset < len(data) {
		b := data[*offset]
		*offset++
		result |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
	}

	return result
}

// ComputeDeterminismHash computes a hash of the Wasm module for determinism verification
func ComputeDeterminismHash(wasm []byte) string {
	hash := sha256.Sum256(wasm)
	return hex.EncodeToString(hash[:])
}

// GetWasmInfo returns information about a Wasm module
func GetWasmInfo(wasm []byte) (map[string]interface{}, error) {
	if err := ValidateWasm(wasm); err != nil {
		return nil, err
	}

	info := make(map[string]interface{})
	info["size"] = len(wasm)
	info["hash"] = ComputeDeterminismHash(wasm)
	info["timestamp"] = time.Now().Unix()

	// Parse sections to get more info
	offset := 8 // Skip magic + version

	for offset < len(wasm) {
		if offset+2 > len(wasm) {
			break
		}

		sectionID := wasm[offset]
		sectionSize := int(wasm[offset+1])

		offset += 2

		if offset+sectionSize > len(wasm) {
			break
		}

		sectionName := getSectionName(sectionID)
		if sectionName != "" {
			info[sectionName] = true
		}

		offset += sectionSize
	}

	return info, nil
}

func getSectionName(id byte) string {
	names := map[byte]string{
		0:  "custom",
		1:  "type",
		2:  "import",
		3:  "function",
		4:  "table",
		5:  "memory",
		6:  "global",
		7:  "export",
		8:  "start",
		9:  "element",
		10: "code",
		11: "data",
	}
	return names[id]
}

// CheckWasmPack checks if wasm-pack is installed
func CheckWasmPack() (bool, error) {
	cmd := exec.Command("wasm-pack", "--version")
	err := cmd.Run()
	if err != nil {
		return false, nil // Not installed
	}
	return true, nil
}

// CheckCargo checks if cargo is installed
func CheckCargo() (bool, error) {
	cmd := exec.Command("cargo", "--version")
	err := cmd.Run()
	if err != nil {
		return false, nil
	}
	return true, nil
}

// CheckRustTarget checks if the specified Rust target is installed
func CheckRustTarget(target string) (bool, error) {
	cmd := exec.Command("rustup", "target", "list", "--installed")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	return strings.Contains(string(output), target), nil
}

// InstallWasmTarget installs the wasm32-unknown-unknown target
func InstallWasmTarget() error {
	cmd := exec.Command("rustup", "target", "add", "wasm32-unknown-unknown")
	return cmd.Run()
}
