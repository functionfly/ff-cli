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
	"sync"
	"time"

	"github.com/sirupsen/logrus"

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
	return compileRustWithMode(ctx, source, target, mode, nil)
}

// CompileRustWithModeCtxWithLog is like CompileRustWithModeCtx but writes any
// cargo stderr output to the provided logger at debug level. The full cargo
// output is never returned to the caller to avoid leaking generated source
// into user-facing error messages.
func CompileRustWithModeCtxWithLog(ctx context.Context, source string, target string, mode string, log *logrus.Logger) ([]byte, error) {
	return compileRustWithMode(ctx, source, target, mode, log)
}

func compileRustWithMode(ctx context.Context, source string, target string, mode string, log *logrus.Logger) ([]byte, error) {
	// Create a temporary directory for the Rust project
	tempDir, err := os.MkdirTemp("", "flypy-rust-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Cargo.toml with dependencies based on mode
	cargoToml := generateCargoToml(mode)
	if err := os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		return nil, fmt.Errorf("failed to write Cargo.toml: %w", err)
	}

	// Create src directory
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create src directory: %w", err)
	}

	// Write lib.rs
	if err := os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(source), 0600); err != nil {
		return nil, fmt.Errorf("failed to write lib.rs: %w", err)
	}

	// Use WASI target for compilation (wasm32-wasip1)
	wasiTarget := "wasm32-wasip1"
	wasmBytes, wasiOutput, wasiErr := compileWithCargoWASI(ctx, tempDir, wasiTarget, log)
	if wasiErr == nil {
		if err := ValidateEntryPoints(wasmBytes); err != nil {
			return nil, fmt.Errorf("WASM validation failed: %w", err)
		}
		return wasmBytes, nil
	}

	// Fallback: try standard wasm32-unknown-unknown target
	wasmBytes, stdOutput, stdErr := compileWithCargo(ctx, tempDir, "wasm32-unknown-unknown", log)
	if stdErr == nil {
		if err := ValidateEntryPoints(wasmBytes); err != nil {
			return nil, fmt.Errorf("WASM validation failed: %w", err)
		}
		return wasmBytes, nil
	}

	// Both targets failed. We deliberately do NOT include the cargo output
	// in the user-facing error (the generated Rust source may be in there).
	// The detailed cargo output is logged at debug level so operators can
	// diagnose failures without exposing source to API consumers.
	debugLog(log, "WASI cargo output", wasiOutput)
	debugLog(log, "standard cargo output", stdOutput)
	return nil, fmt.Errorf("failed to compile WASM module (WASI: %v; wasm32-unknown-unknown: %v)", wasiErr, stdErr)
}

func compileWithCargoWASI(ctx context.Context, tempDir string, target string, log *logrus.Logger) ([]byte, []byte, error) {
	cargoPath, err := resolveCargo()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, cargoPath, "build", "--release", "--target", target, "--message-format=short")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "RUSTFLAGS=-C target-feature=-crt-static", "CARGO_TERM_COLOR=never")
	cmd.Stdin = nil

	output, err := cmd.CombinedOutput()
	if err != nil {
		debugLog(log, "cargo build (WASI) failed", output)
		hint := "   -> Install the WASI target with: rustup target add wasm32-wasip1"
		if _, hasRustup := hasRustup(); !hasRustup {
			hint = "   -> Install Rust with WASI support: https://rustup.rs"
		}
		return nil, output, fmt.Errorf("cargo build for WASI failed: %w%s", err, hint)
	}

	wasmPath := filepath.Join(tempDir, "target", target, "release", "flypy_function.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		wasmPath = filepath.Join(tempDir, "target", target, "release", "deps", "flypy_function.wasm")
		if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
			return nil, output, fmt.Errorf("Wasm file not found after WASI build")
		}
	}

	bytes, err := os.ReadFile(wasmPath)
	return bytes, output, err
}

func compileWithCargo(ctx context.Context, tempDir string, target string, log *logrus.Logger) ([]byte, []byte, error) {
	cargoPath, err := resolveCargo()
	if err != nil {
		return nil, nil, err
	}
	var cmd *exec.Cmd
	if target != "" {
		cmd = exec.CommandContext(ctx, cargoPath, "build", "--release", "--target", target, "--message-format=short")
	} else {
		cmd = exec.CommandContext(ctx, cargoPath, "build", "--release", "--message-format=short")
	}

	cmd.Dir = tempDir
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "CARGO_TERM_COLOR=never")
	output, err := cmd.CombinedOutput()
	if err != nil {
		debugLog(log, "cargo build (standard) failed", output)
		hint := "   -> Install the WASM target with: rustup target add wasm32-unknown-unknown"
		if _, hasRustup := hasRustup(); !hasRustup {
			hint = "   -> Install Rust with WASI support: https://rustup.rs"
		}
		return nil, output, fmt.Errorf("cargo build failed: %w%s", err, hint)
	}

	wasmPath := filepath.Join(tempDir, "target", target, "release", "flypy_function.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		return nil, output, fmt.Errorf("Wasm file not found after standard build")
	}

	bytes, err := os.ReadFile(wasmPath)
	return bytes, output, err
}

// debugLog writes the cargo output to the structured logger at debug level
// if a logger is provided. The output is truncated to avoid huge logs.
func debugLog(log *logrus.Logger, msg string, output []byte) {
	if log == nil || len(output) == 0 {
		return
	}
	const maxBytes = 8 * 1024
	if len(output) > maxBytes {
		output = append(output[:maxBytes], []byte("\n... (truncated)")...)
	}
	log.WithField("output", string(output)).Debug(msg)
}

// ValidateWasm checks if the given bytes represent a valid Wasm module
func ValidateWasm(wasm []byte) error {
	if len(wasm) < 8 {
		return fmt.Errorf("Wasm module too short")
	}

	magic := []byte{0x00, 0x61, 0x73, 0x6d}
	if !bytes.Equal(wasm[:4], magic) {
		return fmt.Errorf("invalid Wasm magic number")
	}

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

	count := readVarint(data, &offset)

	for i := 0; i < count; i++ {
		if offset >= len(data) {
			break
		}

		nameLen := readVarint(data, &offset)
		if offset+nameLen > len(data) {
			break
		}

		name := string(data[offset : offset+nameLen])
		exports = append(exports, name)
		offset += nameLen

		if offset >= len(data) {
			break
		}
		offset++ // skip kind

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
	path, err := resolveWasmPack()
	if err != nil {
		return false, nil
	}
	cmd := exec.Command(path, "--version")
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

// CheckCargo checks if cargo is installed
func CheckCargo() (bool, error) {
	path, err := resolveCargo()
	if err != nil {
		return false, nil
	}
	cmd := exec.Command(path, "--version")
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

// CheckRustTarget checks if the specified Rust target is installed
func CheckRustTarget(target string) (bool, error) {
	path, err := resolveRustup()
	if err != nil {
		return false, err
	}
	cmd := exec.Command(path, "target", "list", "--installed")
	cmd.Stdin = nil
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	return strings.Contains(string(output), target), nil
}

// InstallWasmTarget installs the wasm32-unknown-unknown target
func InstallWasmTarget() error {
	path, err := resolveRustup()
	if err != nil {
		return err
	}
	cmd := exec.Command(path, "target", "add", "wasm32-unknown-unknown")
	cmd.Stdin = nil
	return cmd.Run()
}

// hasRustup reports whether the rustup binary is on PATH.
func hasRustup() (string, bool) {
	path, err := resolveRustup()
	if err != nil {
		return "", false
	}
	return path, true
}

// resolveCargo returns the absolute path to cargo, resolved once.
func resolveCargo() (string, error) {
	if p := cachedExec("cargo"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("cargo")
	if err != nil {
		return "", fmt.Errorf("cargo not found in PATH: %w", err)
	}
	cacheExec("cargo", p)
	return p, nil
}

// resolveRustup returns the absolute path to rustup, resolved once.
func resolveRustup() (string, error) {
	if p := cachedExec("rustup"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("rustup")
	if err != nil {
		return "", fmt.Errorf("rustup not found in PATH: %w", err)
	}
	cacheExec("rustup", p)
	return p, nil
}

// resolveWasmPack returns the absolute path to wasm-pack, resolved once.
func resolveWasmPack() (string, error) {
	if p := cachedExec("wasm-pack"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("wasm-pack")
	if err != nil {
		return "", fmt.Errorf("wasm-pack not found in PATH: %w", err)
	}
	cacheExec("wasm-pack", p)
	return p, nil
}

var (
	execPathCacheMu sync.Mutex
	execPathCache   = map[string]string{}
)

func cacheExec(name, path string) {
	execPathCacheMu.Lock()
	execPathCache[name] = path
	execPathCacheMu.Unlock()
}

func cachedExec(name string) string {
	execPathCacheMu.Lock()
	p := execPathCache[name]
	execPathCacheMu.Unlock()
	return p
}
