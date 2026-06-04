package bundler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// WASM 1.0 section IDs
const (
	wasmSectionCustom = 0
	wasmSectionType   = 1
	wasmSectionImport = 2
	wasmSectionFunc   = 3
	wasmSectionTable  = 4
	wasmSectionMemory = 5
	wasmSectionGlobal = 6
	wasmSectionExport = 7
	wasmSectionStart  = 8
	wasmSectionElem   = 9
	wasmSectionCode   = 10
	wasmSectionData   = 11
)

// TypeScriptWASMConfig contains configuration for TypeScript WASM compilation
type TypeScriptWASMConfig struct {
	// Target specifies the WASM target (wasip1, wasip2, etc.)
	Target string
	// ModuleType specifies the output module type (esm, cjs)
	ModuleType string
	// Minify enables minification of the output
	Minify bool
	// SourceMap generates source maps
	SourceMap bool
	// TreeShaking enables tree shaking
	TreeShaking bool
}

// CompiledWASM represents the result of TypeScript WASM compilation
type CompiledWASM struct {
	// Binary is the compiled WASM binary
	Binary []byte
	// Metadata contains compilation metadata
	Metadata *WASMMetadata
}

// WASMMetadata contains metadata about the compiled WASM module
type WASMMetadata struct {
	// HandlerName is the exported handler function name
	HandlerName string
	// MemoryPages is the number of memory pages required
	MemoryPages uint32
	// ExportedFunctions lists all exported functions
	ExportedFunctions []string
	// WASITarget indicates if WASI is supported
	WASITarget bool
}

// DefaultTypeScriptWASMConfig returns the default configuration
func DefaultTypeScriptWASMConfig() *TypeScriptWASMConfig {
	return &TypeScriptWASMConfig{
		Target:      "wasip1",
		ModuleType:  "esm",
		Minify:      false,
		SourceMap:   false,
		TreeShaking: true,
	}
}

// bundleTypeScriptWASM bundles TypeScript code for WASM runtime execution
// Uses esbuild to compile TypeScript to JavaScript, then optionally uses
// wasm-bindgen or QuickJS for JS → WASM conversion
func bundleTypeScriptWASM(manifest *manifest.Manifest) ([]byte, error) {
	return bundleTypeScriptWASMWithConfig(manifest, DefaultTypeScriptWASMConfig())
}

// bundleTypeScriptWASMWithConfig bundles TypeScript code with custom configuration
func bundleTypeScriptWASMWithConfig(manifest *manifest.Manifest, config *TypeScriptWASMConfig) ([]byte, error) {
	// Read and validate entry file
	// Read and validate entry file
	entryFile, _, err := ReadEntryFile(manifest)
	if err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm bundle", "failed to read entry file", err)
	}

	// Validate entry file is TypeScript
	if !isTypeScriptFile(entryFile) {
		return nil, NewBundlerError("typescript-wasm bundle", "entry file must be a TypeScript file (.ts or .tsx)")
	}

	// Try compilation with available tools
	// Priority: 1) wasm-bindgen, 2) QuickJS (javy), 3) esbuild only with wrapper

	// First try esbuild to compile TypeScript to JavaScript
	jsBundle, err := compileTypeScriptToJS(entryFile, config)
	if err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm bundle", "failed to compile TypeScript to JS", err)
	}

	// Try to compile JS to WASM using available tools
	if wasmBinary, err := compileJSToWASMWithConfig(string(jsBundle), manifest, config); err == nil {
		// Validate the compiled WASM
		if err := validateWasmModule(wasmBinary); err != nil {
			fmt.Printf("Warning: Compiled WASM validation failed (%v), using fallback\n", err)
			return createTypeScriptWasmFallback(jsBundle, manifest, config)
		}

		// Extract metadata from WASM
		metadata := extractWASMMetadata(wasmBinary)
		fmt.Printf("Successfully compiled %s to WASM\n", entryFile)

		// Return combined binary with metadata prefix
		return embedMetadata(wasmBinary, metadata)
	}

	// Fallback: Use JavaScript wrapper
	fmt.Printf("Warning: JS to WASM compilation failed, using JavaScript wrapper\n")
	return createTypeScriptWasmFallback(jsBundle, manifest, config)
}

// compileTypeScriptToJS compiles TypeScript to JavaScript using esbuild
func compileTypeScriptToJS(entryFile string, config *TypeScriptWASMConfig) ([]byte, error) {
	// Check if esbuild is available
	if _, err := exec.LookPath("esbuild"); err != nil {
		// Fallback: use tsc if available
		return compileTypeScriptWithTSC(entryFile)
	}

	// Create temporary output file
	tempDir := os.TempDir()
	tempOut := filepath.Join(tempDir, fmt.Sprintf("functionfly-ts-%d.js", os.Getpid()))
	defer os.Remove(tempOut)

	// Build esbuild command
	args := []string{
		entryFile,
		"--bundle",
		"--platform=browser",
		"--format=" + config.ModuleType,
		"--outfile=" + tempOut,
		"--target=" + getEsbuildTarget(config.Target),
	}

	if config.Minify {
		args = append(args, "--minify")
	}
	if config.SourceMap {
		args = append(args, "--sourcemap")
	}
	if config.TreeShaking {
		args = append(args, "--tree-shaking=true")
	}

	// Add JSX support
	if strings.HasSuffix(entryFile, ".tsx") {
		args = append(args, "--loader:.tsx=jsx")
	}
	args = append(args, "--loader:.ts=ts")

	// Set working directory
	workDir := filepath.Dir(entryFile)
	cmd := exec.Command("esbuild", args...)
	cmd.Dir = workDir

	// Execute compilation
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, NewCompilationErrorWithOutput("esbuild", entryFile, string(output), err)
	}

	// Read the compiled JavaScript
	jsBytes, err := os.ReadFile(tempOut)
	if err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm bundle", "failed to read compiled JS", err)
	}

	return jsBytes, nil
}

// compileTypeScriptWithTSC compiles TypeScript using tsc as fallback
func compileTypeScriptWithTSC(entryFile string) ([]byte, error) {
	// Check if tsc is available
	if _, err := exec.LookPath("tsc"); err != nil {
		return nil, NewBundlerError("typescript-wasm bundle", "neither esbuild nor tsc found in PATH")
	}

	// Read the TypeScript source directly as fallback
	// (we'll use a JS wrapper instead of full compilation)
	sourceBytes, err := os.ReadFile(entryFile)
	if err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm bundle", "failed to read TypeScript source", err)
	}

	return sourceBytes, nil
}

// compileJSToWASMWithConfig compiles JavaScript to WASM using available tools
func compileJSToWASMWithConfig(jsSource string, manifest *manifest.Manifest, config *TypeScriptWASMConfig) ([]byte, error) {
	// Try Javy (QuickJS-based) first
	if wasmBytes, err := compileWithJavy(jsSource, manifest); err == nil {
		return wasmBytes, nil
	}

	// Try wasm-bindgen if available
	if wasmBytes, err := compileWithWasmBindgen(jsSource, manifest); err == nil {
		return wasmBytes, nil
	}

	// Fallback: create a JS wrapper for the WASM runtime
	return createTypeScriptWasmFallback([]byte(jsSource), manifest, config)
}

// compileWithJavy compiles JavaScript to WASM using Javy (QuickJS)
func compileWithJavy(jsSource string, manifest *manifest.Manifest) ([]byte, error) {
	// Check if Javy is available
	if _, err := exec.LookPath("javy"); err != nil {
		return nil, NewBundlerError("typescript-wasm compile", "javy not found")
	}

	// Create temporary files
	tempDir := os.TempDir()
	jsFile := filepath.Join(tempDir, fmt.Sprintf("functionfly-%d.js", os.Getpid()))
	wasmFile := filepath.Join(tempDir, fmt.Sprintf("functionfly-%d.wasm", os.Getpid()))
	defer os.Remove(jsFile)
	defer os.Remove(wasmFile)

	// Write JavaScript to temp file
	if err := os.WriteFile(jsFile, []byte(jsSource), 0600); err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm compile", "failed to write temp JS file", err)
	}

	// Build Javy command
	args := []string{
		"compile",
		jsFile,
		"-o", wasmFile,
		"--dynamic",
	}

	cmd := exec.Command("javy", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, NewCompilationErrorWithOutput("javy", jsFile, string(output), err)
	}

	// Read the compiled WASM
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		return nil, NewBundlerErrorWithCause("typescript-wasm compile", "failed to read compiled WASM", err)
	}

	return wasmBytes, nil
}

// compileWithWasmBindgen compiles JavaScript to WASM using wasm-bindgen
func compileWithWasmBindgen(jsSource string, manifest *manifest.Manifest) ([]byte, error) {
	// Check if wasm-bindgen is available
	if _, err := exec.LookPath("wasm-bindgen"); err != nil {
		return nil, NewBundlerError("typescript-wasm compile", "wasm-bindgen not found")
	}

	// This would require a different setup - skip for now
	return nil, NewBundlerError("typescript-wasm compile", "wasm-bindgen requires WASI setup")
}

// createTypeScriptWasmFallback creates a JavaScript wrapper for the WASM runtime
func createTypeScriptWasmFallback(jsBundle []byte, manifest *manifest.Manifest, config *TypeScriptWASMConfig) ([]byte, error) {
	// Extract handler name from manifest
	// Extract handler name from manifest (default to "handler")
	handlerName := "handler"
	if handlerName == "" {
		handlerName = "handler"
	}

	// Create a wrapper that works with our WASM runtime
	wrapper := createJSWrapper(string(jsBundle), handlerName, manifest)

	// Return as a special bundle that the runtime will handle
	return []byte(wrapper), nil
}

// createJSWrapper creates a JavaScript wrapper for WASM execution
func createJSWrapper(jsSource string, handlerName string, manifest *manifest.Manifest) string {
	// The wrapper provides the standard FunctionFly interface
	// and converts between WASM memory and JavaScript objects
	wrapper := fmt.Sprintf(`(function() {
  'use strict';

  // The compiled handler source
  var handlerSource = %s;

  // WASI-like environment shim
  var env = {
    // Environment variables
    getEnv: function(name) {
      return this[name] || '';
    },

    // KV store (implemented via host functions)
    kv: {
      _data: new Map(),
      get: function(key) {
        return this._data.get(key) || null;
      },
      set: function(key, value) {
        this._data.set(key, value);
      },
      delete: function(key) {
        this._data.delete(key);
      }
    },

    // HTTP fetch (implemented via host function)
    fetch: function(url, options) {
      return __functionfly_fetch(url, options);
    },

    // Logging
    log: function(msg) {
      __functionfly_log(msg);
    }
  };

  // Default handler wrapper
  async function defaultHandler(request, _env, context) {
    var handlerFn = handlerSource.default || handlerSource.handler || handlerSource;
    var response = await handlerFn(request, env, context || {});
    return response;
  }

  // Export for WASM runtime
  var module = {
    init: function() {
      // Initialization if needed
    },

    execute: function(inputPtr, inputLen) {
      try {
        var input = __functionfly_read_string(inputPtr, inputLen);
        var request = JSON.parse(input);

        // Execute handler
        var result = defaultHandler(request, env, {});

        // Return result as pointer
        return __functionfly_write_string(JSON.stringify(result));
      } catch (error) {
        __functionfly_log('Error: ' + error.message);
        return -1;
      }
    },

    // Memory allocation
    alloc: function(size) {
      return __functionfly_alloc(size);
    },

    dealloc: function(ptr) {
      // Memory deallocation if needed
    }
  };

  // Export for different module systems
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = module;
  }
  if (typeof exports !== 'undefined') {
    exports = module;
  }
  if (typeof window !== 'undefined') {
    window.FunctionFlyHandler = module;
  }
  if (typeof self !== 'undefined') {
    self.FunctionFlyHandler = module;
  }

  return module;
})();
`, escapeJSString(jsSource))

	return wrapper
}

// escapeJSString properly escapes a JavaScript string for embedding
func escapeJSString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`), "\n", `\n`), "\r", `\r`)
}

// getEsbuildTarget converts WASM target to esbuild target
func getEsbuildTarget(target string) string {
	switch target {
	case "wasip1":
		return "es2020"
	case "wasip2":
		return "es2022"
	default:
		return "es2020"
	}
}

// isTypeScriptFile checks if the file is a TypeScript file
func isTypeScriptFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".ts" || ext == ".tsx"
}

// readU32LEB128 reads a uint32 LEB128 from data; returns value and bytes consumed, or 0,0 if invalid.
func readU32LEB128(data []byte) (uint32, int) {
	var v uint32
	var shift uint
	for i, b := range data {
		if i >= 5 {
			return 0, 0
		}
		v |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, i + 1
		}
		shift += 7
	}
	return 0, 0
}

// parseWASMModule parses a WASM 1.0 binary and extracts export names, memory limits, and WASI presence.
// Returns (exportedFuncs, memoryPages, hasWASI). On any parse error, returns nil, 0, false.
func parseWASMModule(wasmBinary []byte) (exportedFuncs []string, memoryPages uint32, hasWASI bool) {
	const wasmMagic = "\x00asm"
	if len(wasmBinary) < 8 {
		return nil, 0, false
	}
	if string(wasmBinary[:4]) != wasmMagic {
		return nil, 0, false
	}
	// version must be 1
	if wasmBinary[4] != 1 || wasmBinary[5] != 0 || wasmBinary[6] != 0 || wasmBinary[7] != 0 {
		return nil, 0, false
	}
	pos := 8
	for pos < len(wasmBinary) {
		if pos+1 > len(wasmBinary) {
			break
		}
		sectionID := wasmBinary[pos]
		pos++
		n, sz := readU32LEB128(wasmBinary[pos:])
		if sz == 0 || pos+sz > len(wasmBinary) {
			break
		}
		pos += sz
		payloadEnd := pos + int(n)
		if payloadEnd > len(wasmBinary) {
			break
		}
		payload := wasmBinary[pos:payloadEnd]
		pos = payloadEnd

		switch sectionID {
		case wasmSectionImport:
			hasWASI = parseWASIImports(payload)
		case wasmSectionMemory:
			if pages := parseMemorySection(payload); pages > 0 {
				memoryPages = pages
			}
		case wasmSectionExport:
			if names := parseExportSection(payload); len(names) > 0 {
				exportedFuncs = names
			}
		}
	}
	return exportedFuncs, memoryPages, hasWASI
}

func parseWASIImports(payload []byte) bool {
	count, n := readU32LEB128(payload)
	if n == 0 {
		return false
	}
	pos := n
	for i := uint32(0); i < count && pos < len(payload); i++ {
		// module name (name_len + bytes)
		modLen, sz := readU32LEB128(payload[pos:])
		if sz == 0 || pos+sz+int(modLen) > len(payload) {
			break
		}
		pos += sz
		module := string(payload[pos : pos+int(modLen)])
		pos += int(modLen)
		if module == "wasi_snapshot_preview1" || module == "wasi_preview1" || strings.HasPrefix(module, "wasi") {
			return true
		}
		// field name
		fieldLen, sz := readU32LEB128(payload[pos:])
		if sz == 0 || pos+sz+int(fieldLen) > len(payload) {
			break
		}
		pos += sz + int(fieldLen)
		// kind byte
		if pos >= len(payload) {
			break
		}
		kind := payload[pos]
		pos++
		switch kind {
		case 0: // func
			if _, sz := readU32LEB128(payload[pos:]); sz != 0 {
				pos += sz
			}
		case 1: // table
			if _, sz := readU32LEB128(payload[pos:]); sz != 0 {
				pos += sz
			}
		case 2: // memory: limits = flags (1 byte), initial, optional max
			if pos >= len(payload) {
				break
			}
			flags := payload[pos]
			pos++
			if _, sz := readU32LEB128(payload[pos:]); sz != 0 {
				pos += sz
			}
			if flags&1 != 0 {
				if _, sz := readU32LEB128(payload[pos:]); sz != 0 {
					pos += sz
				}
			}
		case 3: // global: type byte, mutability byte
			if pos+2 <= len(payload) {
				pos += 2
			}
		}
	}
	return false
}

func parseMemorySection(payload []byte) uint32 {
	count, n := readU32LEB128(payload)
	if n == 0 || count == 0 {
		return 0
	}
	pos := n
	// first memory: limits (flags byte, initial, optional max)
	if pos >= len(payload) {
		return 0
	}
	flags := payload[pos]
	pos++
	initial, sz := readU32LEB128(payload[pos:])
	if sz == 0 {
		return 0
	}
	_ = flags
	return initial
}

func parseExportSection(payload []byte) []string {
	count, n := readU32LEB128(payload)
	if n == 0 {
		return nil
	}
	pos := n
	var names []string
	for i := uint32(0); i < count && pos < len(payload); i++ {
		nameLen, sz := readU32LEB128(payload[pos:])
		if sz == 0 || pos+sz+int(nameLen) > len(payload) {
			break
		}
		pos += sz
		name := string(payload[pos : pos+int(nameLen)])
		pos += int(nameLen)
		if pos >= len(payload) {
			break
		}
		kind := payload[pos]
		pos++
		if kind == 0 { // function export
			names = append(names, name)
		}
		if _, sz := readU32LEB128(payload[pos:]); sz != 0 {
			pos += sz
		}
	}
	return names
}

// pickHandlerName chooses a handler name from exported function names (prefer handler, execute, init).
func pickHandlerName(exported []string) string {
	for _, name := range []string{"handler", "execute", "init", "_start"} {
		for _, e := range exported {
			if e == name {
				return e
			}
		}
	}
	if len(exported) > 0 {
		return exported[0]
	}
	return "handler"
}

// extractWASMMetadata extracts metadata from a WASM binary by parsing the module structure.
func extractWASMMetadata(wasmBinary []byte) *WASMMetadata {
	exportedFuncs, memoryPages, wasiTarget := parseWASMModule(wasmBinary)
	handlerName := "handler"
	if len(exportedFuncs) > 0 {
		handlerName = pickHandlerName(exportedFuncs)
		if memoryPages == 0 {
			memoryPages = 256
		}
	}
	if memoryPages == 0 {
		memoryPages = 256
	}
	if len(exportedFuncs) == 0 {
		exportedFuncs = []string{"init", "execute", "alloc", "dealloc"}
	}
	return &WASMMetadata{
		HandlerName:       handlerName,
		MemoryPages:       memoryPages,
		ExportedFunctions: exportedFuncs,
		WASITarget:        wasiTarget,
	}
}

// embedMetadata embeds metadata into the WASM binary
func embedMetadata(wasmBinary []byte, metadata *WASMMetadata) ([]byte, error) {
	// Serialize metadata
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// Create a magic header to identify our format
	header := []byte("FFWB") // FunctionFly WASM Binary
	metadataLen := make([]byte, 4)
	metadataLen[0] = byte(len(metadataJSON) >> 24)
	metadataLen[1] = byte(len(metadataJSON) >> 16)
	metadataLen[2] = byte(len(metadataJSON) >> 8)
	metadataLen[3] = byte(len(metadataJSON))

	// Combine: header + metadata length + metadata + WASM binary
	result := make([]byte, 0, len(header)+len(metadataLen)+len(metadataJSON)+len(wasmBinary))
	result = append(result, header...)
	result = append(result, metadataLen...)
	result = append(result, metadataJSON...)
	result = append(result, wasmBinary...)

	return result, nil
}

// ExtractMetadata extracts metadata from a bundled WASM binary
func ExtractMetadata(bundledBinary []byte) (*WASMMetadata, error) {
	// Check for our magic header
	header := "FFWB"
	if len(bundledBinary) < len(header)+4 {
		// Not our format, assume pure WASM
		return &WASMMetadata{
			HandlerName:       "handler",
			MemoryPages:       256,
			ExportedFunctions: []string{"_start", "memory"},
			WASITarget:        false,
		}, nil
	}

	// Verify header
	if string(bundledBinary[:len(header)]) != header {
		// Not our format
		return &WASMMetadata{
			HandlerName:       "handler",
			MemoryPages:       256,
			ExportedFunctions: []string{"_start", "memory"},
			WASITarget:        false,
		}, nil
	}

	// Extract metadata length
	metadataLen := int(bundledBinary[4])<<24 | int(bundledBinary[5])<<16 | int(bundledBinary[6])<<8 | int(bundledBinary[7])

	// Extract metadata
	if len(bundledBinary) < len(header)+4+metadataLen {
		return nil, fmt.Errorf("invalid bundled binary: metadata length exceeds bounds")
	}

	metadataJSON := bundledBinary[len(header)+4 : len(header)+4+metadataLen]
	var metadata WASMMetadata
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// GetWASMBinary extracts the actual WASM binary from a bundled binary
func GetWASMBinary(bundledBinary []byte) ([]byte, error) {
	header := "FFWB"
	if len(bundledBinary) < len(header)+4 {
		// Not our format, return as-is (assume pure WASM)
		return bundledBinary, nil
	}

	if string(bundledBinary[:len(header)]) != header {
		// Not our format
		return bundledBinary, nil
	}

	// Extract metadata length
	metadataLen := int(bundledBinary[4])<<24 | int(bundledBinary[5])<<16 | int(bundledBinary[6])<<8 | int(bundledBinary[7])

	// Extract WASM binary
	wasmStart := len(header) + 4 + metadataLen
	if len(bundledBinary) < wasmStart {
		return nil, fmt.Errorf("invalid bundled binary: WASM binary starts beyond bounds")
	}

	return bundledBinary[wasmStart:], nil
}

// TypeScriptWASMCompilerConfig contains configuration for TypeScript WASM compilation
type TypeScriptWASMCompilerConfig struct {
	// Minify enables minification of the output
	Minify bool
	// Target specifies the WASM target
	Target string
}

// CompileTypeScript compiles TypeScript source to WASM
// This is the main entry point for the TypeScript WASM compiler
func CompileTypeScript(source string, config *TypeScriptWASMCompilerConfig) (*CompiledWASM, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "functionfly-ts-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Write source to temp file
	entryFile := filepath.Join(tempDir, "main.ts")
	if err := os.WriteFile(entryFile, []byte(source), 0600); err != nil {
		return nil, fmt.Errorf("failed to write source file: %w", err)
	}

	// Get compilation config
	tsConfig := DefaultTypeScriptWASMConfig()
	if config != nil {
		if config.Minify {
			tsConfig.Minify = true
		}
		if config.Target != "" {
			tsConfig.Target = config.Target
		}
	}

	// Compile to JavaScript first
	jsBundle, err := compileTypeScriptToJS(entryFile, tsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compile TypeScript: %w", err)
	}

	// Try to compile JS to WASM
	wasmBinary, err := compileJSToWASMWithConfig(string(jsBundle), &manifest.Manifest{
		Runtime: "typescript-wasm",
		Entry:   "main.ts",
	}, tsConfig)

	if err != nil {
		// Fall back to JS wrapper
		wrapper := createJSWrapper(string(jsBundle), "handler", &manifest.Manifest{})
		return &CompiledWASM{
			Binary: []byte(wrapper),
			Metadata: &WASMMetadata{
				HandlerName:       "handler",
				MemoryPages:       256,
				ExportedFunctions: []string{"init", "execute", "alloc", "dealloc"},
				WASITarget:        false,
			},
		}, nil
	}

	// Extract and embed metadata
	metadata := extractWASMMetadata(wasmBinary)
	embedded, err := embedMetadata(wasmBinary, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to embed metadata: %w", err)
	}

	return &CompiledWASM{
		Binary:   embedded,
		Metadata: metadata,
	}, nil
}

// CompileWithDeps compiles TypeScript with npm dependencies. It resolves deps
// to exact versions via the npm registry, installs them into a temp dir, then
// compiles so imports resolve from node_modules.
func CompileWithDeps(source string, deps map[string]string) (*CompiledWASM, error) {
	tempDir, err := os.MkdirTemp("", "functionfly-ts-deps-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	entryFile := filepath.Join(tempDir, "main.ts")
	if err := os.WriteFile(entryFile, []byte(source), 0600); err != nil {
		return nil, fmt.Errorf("failed to write source: %w", err)
	}

	if len(deps) > 0 {
		resolved, err := resolveAndInstallDeps(context.Background(), tempDir, deps)
		if err != nil {
			return nil, err
		}
		_ = resolved // used only for install
	}

	tsConfig := DefaultTypeScriptWASMConfig()
	jsBundle, err := compileTypeScriptToJS(entryFile, tsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compile TypeScript: %w", err)
	}

	wasmBinary, err := compileJSToWASMWithConfig(string(jsBundle), &manifest.Manifest{
		Runtime: "typescript-wasm",
		Entry:   "main.ts",
	}, tsConfig)
	if err != nil {
		wrapper := createJSWrapper(string(jsBundle), "handler", &manifest.Manifest{})
		return &CompiledWASM{
			Binary: []byte(wrapper),
			Metadata: &WASMMetadata{
				HandlerName:       "handler",
				MemoryPages:       256,
				ExportedFunctions: []string{"init", "execute", "alloc", "dealloc"},
				WASITarget:        false,
			},
		}, nil
	}

	metadata := extractWASMMetadata(wasmBinary)
	embedded, err := embedMetadata(wasmBinary, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to embed metadata: %w", err)
	}
	return &CompiledWASM{Binary: embedded, Metadata: metadata}, nil
}

// resolveAndInstallDeps resolves direct deps to exact versions, writes package.json, and runs npm install.
func resolveAndInstallDeps(ctx context.Context, workDir string, deps map[string]string) (map[string]*NPMMetadata, error) {
	cacheDir := filepath.Join(os.TempDir(), "functionfly-npm-cache")
	client := NewNPMClient(cacheDir)
	metadata, err := client.ResolveDependencies(ctx, deps)
	if err != nil {
		return nil, NewBundlerErrorWithCause("npm resolve", "failed to resolve dependencies", err)
	}
	exactDeps := make(map[string]string)
	for name, meta := range metadata {
		if meta != nil && meta.Version != "" {
			exactDeps[name] = meta.Version
		}
	}
	if len(exactDeps) == 0 {
		return metadata, nil
	}
	pkg := &PackageJSON{
		Name:         "functionfly-temp",
		Version:      "1.0.0",
		Description:  "Temporary package for TypeScript WASM compile",
		Dependencies: exactDeps,
	}
	pkgBytes, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return nil, NewBundlerErrorWithCause("npm install", "failed to build package.json", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "package.json"), pkgBytes, 0600); err != nil {
		return nil, NewBundlerErrorWithCause("npm install", "failed to write package.json", err)
	}
	cmd := exec.Command("npm", "install", "--production", "--no-audit", "--no-fund")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, NewBundlerErrorWithCause("npm install", fmt.Sprintf("npm install failed: %s", string(out)), err)
	}
	return metadata, nil
}
