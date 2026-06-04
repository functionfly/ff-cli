package bundler

import (
	"fmt"
	"os"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// bundlePythonForWasmRuntime bundles Python for Wasm runtime execution
// Uses MicroPython runtime - FlyPy has been disabled
func bundlePythonForWasmRuntime(manifest *manifest.Manifest) ([]byte, error) {
	// Read and validate entry file using shared helper
	entryFile, sourceCode, err := ReadEntryFile(manifest)
	if err != nil {
		return nil, NewBundlerErrorWithCause("wasm python bundle", "failed to read entry file", err)
	}

	fmt.Printf("Bundling Python to WASM using MicroPython for %s\n", entryFile)

	// Use MicroPython runtime to bundle Python
	fmt.Printf("Using MicroPython runtime for %s\n", entryFile)
	if wasmBytes, err := createPythonWasmWithRuntime(string(sourceCode), manifest); err == nil {
		if err := validateWasmModule(wasmBytes); err != nil {
			fmt.Printf("Warning: MicroPython WASM validation failed (%v)\n", err)
		} else {
			fmt.Printf("Successfully created Python WASM module with MicroPython runtime\n")
			return wasmBytes, nil
		}
	} else {
		fmt.Printf("Warning: MicroPython runtime approach failed (%v)\n", err)
	}

	// No more fallbacks - return clear error
	return nil, NewBundlerErrorWithCause("wasm python bundle",
		"compilation failed - MicroPython runtime unavailable",
		fmt.Errorf("Micropython: provide micropython.wasm in bundler/python/"))
}

// Simplified: Now using only MicroPython (FlyPy has been disabled)

// createPythonWasmWithRuntime creates a WASM module using production MicroPython runtime
func createPythonWasmWithRuntime(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Production approach: Use MicropythonLinker to create a wrapper module
	// that links with the real MicroPython runtime at execution time
	fmt.Printf("Using production MicroPython runtime for %s\n", manifest.Name)

	// Use the MicropythonLinker to create a proper wrapper module
	linker := NewMicropythonLinker(sourceCode, manifest)
	wasmBytes, err := linker.Link()
	if err != nil {
		return nil, fmt.Errorf("failed to create MicroPython wrapper module: %v", err)
	}

	// Validate the generated module
	if err := validateWasmModule(wasmBytes); err != nil {
		return nil, fmt.Errorf("generated MicroPython module validation failed: %v", err)
	}

	fmt.Printf("Production: Created MicroPython wrapper module (%d bytes) for runtime linking\n", len(wasmBytes))

	return wasmBytes, nil
}

// loadMicropythonRuntime loads the precompiled Micropython WASM runtime
func loadMicropythonRuntime() ([]byte, error) {
	// Try different paths for the runtime
	runtimePaths := []string{
		"bundler/python/micropython.wasm",
		"../../bundler/python/micropython.wasm",
		"internal/bundler/python/micropython.wasm",
	}

	for _, path := range runtimePaths {
		if bytes, err := os.ReadFile(path); err == nil {
			// Basic validation - check WASM magic bytes
			if len(bytes) > 8 && bytes[0] == 0x00 && bytes[1] == 0x61 && bytes[2] == 0x73 && bytes[3] == 0x6D {
				return bytes, nil
			}
		}
	}

	return nil, fmt.Errorf("Micropython runtime not found. Please build micropython.wasm and place in bundler/python/")
}

// createRuntimeWithEmbeddedCode combines runtime with embedded user code
func createRuntimeWithEmbeddedCode(runtimeBytes []byte, sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Phase 4.1: Use precompiled runtime with embedded user code
	// The runtime already has the interface: init, execute, load_code, alloc, dealloc, metadata
	// We embed user code directly into the runtime's data section

	// Create metadata JSON
	metadata := fmt.Sprintf(`{
		"name": "%s",
		"runtime": "python-precompiled",
		"runtime_version": "micropython-1.20",
		"version": "%s",
		"entry_point": "handler",
		"dependencies": [],
		"memory_mb": 128,
		"timeout_ms": 5000,
		"uses_network": false,
		"uses_filesystem": false,
		"phase": "4.1-precompiled-runtime"
	}`, manifest.Name, manifest.Version)

	// Create a WAT module that uses the precompiled runtime directly
	// This module embeds user code and calls runtime functions
	watTemplate := fmt.Sprintf(`
(module
  ;; Import precompiled Micropython runtime
  (import "micropython" "memory" (memory 1))
  (import "micropython" "init" (func $mp_init))
  (import "micropython" "load_code" (func $mp_load_code (param i32 i32)))
  (import "micropython" "execute" (func $mp_execute (param i32 i32) (result i32)))
  (import "micropython" "alloc" (func $mp_alloc (param i32) (result i32)))
  (import "micropython" "dealloc" (func $mp_dealloc (param i32)))
  (import "micropython" "metadata" (func $mp_metadata (result i32)))

  ;; Export memory for host access
  (export "memory" (memory 0))

  ;; Global variables
  (global $code_loaded (mut i32) (i32.const 0))

  ;; Embedded user Python code at offset 1024
  (data (i32.const 1024) "%s")

  ;; Embedded metadata at offset 8192
  (data (i32.const 8192) "%s")

  ;; Initialize function
  (func $init (export "init")
    ;; Initialize Micropython runtime
    call $mp_init
    ;; Load user code
    i32.const 1024
    i32.const %d
    call $mp_load_code
    i32.const 1
    global.set $code_loaded
  )

  ;; Execute function
  (func $execute (export "execute") (param $input i32) (param $input_len i32) (result i32)
    local.get $input
    local.get $input_len
    call $mp_execute
  )

  ;; Alloc function
  (func $alloc (export "alloc") (param $size i32) (result i32)
    local.get $size
    call $mp_alloc
  )

  ;; Dealloc function
  (func $dealloc (export "dealloc") (param $ptr i32)
    local.get $ptr
    call $mp_dealloc
  )

  ;; Metadata export
  (func $metadata (export "metadata") (result i32)
    i32.const 8192
  )
)`, escapeForWAT(sourceCode), escapeForWAT(metadata), len(sourceCode))

	// Compile WAT to WASM as relocatable object for linking with precompiled runtime
	wasmBytes, err := compileWATToWasmRelocatable(watTemplate)
	if err != nil {
		// Fallback to standard compilation if relocatable fails
		wasmBytes, err = compileWATToWasm(watTemplate)
		if err != nil {
			return nil, fmt.Errorf("failed to compile WAT to WASM: %v", err)
		}
	}

	return wasmBytes, nil
}

// createPythonWasmModule creates a WASM module for Python execution
func createPythonWasmModule(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Create a WASM module following the standardized FunctionModule interface
	// This generates WebAssembly Text (WAT) that can be compiled to WASM bytecode

	// Escape the source code for WAT data section
	escapedSource := escapeForWAT(sourceCode)

	// Create metadata JSON
	metadata := fmt.Sprintf(`{
		"name": "%s",
		"runtime": "python",
		"runtime_version": "micropython-1.20",
		"version": "%s",
		"entry_point": "handler",
		"dependencies": [],
		"memory_mb": 128,
		"timeout_ms": 5000,
		"uses_network": false,
		"uses_filesystem": false
	}`, manifest.Name, manifest.Version)

	escapedMetadata := escapeForWAT(metadata)

	watTemplate := `
(module
  ;; Memory export (required for all function modules)
  (memory (export "memory") 1)  ;; 64KB pages

  ;; Global variables for memory management
  (global $initialized (mut i32) (i32.const 0))
  (global $input_ptr (mut i32) (i32.const 0))
  (global $output_ptr (mut i32) (i32.const 2048))

  ;; Data sections
  ;; Python source code embedded in WASM
  (data (i32.const 1024) "%s")
  ;; Function metadata
  (data (i32.const 4096) "%s")
  ;; Result buffer
  (data (i32.const 2048) "")

  ;; Initialize function - called once on cold start
  (func $init (export "init")
    ;; Mark as initialized
    i32.const 1
    global.set $initialized

    ;; Initialize Micropython runtime (stub - would call actual Micropython init)
    ;; For now, just return success
  )

  ;; Execute function - main entry point for function execution
  (func $execute (export "execute") (param $input i32) (param $input_len i32) (result i32)
    ;; Store input parameters
    local.get $input
    global.set $input_ptr

    ;; Check if initialized
    global.get $initialized
    i32.eqz
    if
      ;; Auto-initialize if not done
      call $init
    end

    ;; Execute Python function (stub implementation)
    ;; In a real implementation, this would:
    ;; 1. Pass input to Micropython interpreter
    ;; 2. Execute the embedded Python code
    ;; 3. Return result via memory

    ;; For now, return a simple success message
    call $stub_execute
  )

  ;; Get metadata function
  (func $metadata (export "metadata") (result i32)
    ;; Return pointer to metadata JSON
    i32.const 4096
  )

  ;; Stub execution function (simplified implementation)
  (func $stub_execute (result i32)
    ;; Write a simple result to memory
    ;; In real implementation, this would execute actual Python code

    ;; Write "Hello from Python WASM!" to output buffer
    i32.const 2048  ;; output_ptr
    i32.const 72   ;; 'H'
    i32.store8

    i32.const 2049
    i32.const 101  ;; 'e'
    i32.store8

    i32.const 2050
    i32.const 108  ;; 'l'
    i32.store8

    i32.const 2051
    i32.const 108  ;; 'l'
    i32.store8

    i32.const 2052
    i32.const 111  ;; 'o'
    i32.store8

    i32.const 2053
    i32.const 32   ;; ' '
    i32.store8

    i32.const 2054
    i32.const 102  ;; 'f'
    i32.store8

    i32.const 2055
    i32.const 114  ;; 'r'
    i32.store8

    i32.const 2056
    i32.const 111  ;; 'o'
    i32.store8

    i32.const 2057
    i32.const 109  ;; 'm'
    i32.store8

    i32.const 2058
    i32.const 32   ;; ' '
    i32.store8

    i32.const 2059
    i32.const 80   ;; 'P'
    i32.store8

    i32.const 2060
    i32.const 121  ;; 'y'
    i32.store8

    i32.const 2061
    i32.const 116  ;; 't'
    i32.store8

    i32.const 2062
    i32.const 104  ;; 'h'
    i32.store8

    i32.const 2063
    i32.const 111  ;; 'o'
    i32.store8

    i32.const 2064
    i32.const 110  ;; 'n'
    i32.store8

    i32.const 2065
    i32.const 32   ;; ' '
    i32.store8

    i32.const 2066
    i32.const 87   ;; 'W'
    i32.store8

    i32.const 2067
    i32.const 65   ;; 'A'
    i32.store8

    i32.const 2068
    i32.const 83   ;; 'S'
    i32.store8

    i32.const 2069
    i32.const 77   ;; 'M'
    i32.store8

    i32.const 2070
    i32.const 33   ;; '!'
    i32.store8

    i32.const 2071
    i32.const 0    ;; null terminator
    i32.store8

    ;; Return output pointer
    global.get $output_ptr
  )
)`

	// Generate the WAT content
	watContent := fmt.Sprintf(watTemplate, escapedSource, escapedMetadata)

	// Compile WAT to WASM bytecode using wat2wasm
	wasmBytes, err := compileWATToWasm(watContent)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WAT to WASM: %v", err)
	}

	return wasmBytes, nil
}

// detectCompilationMode analyzes Python source code to determine the appropriate compilation mode
// Returns "complex" for code that uses CSV, IO, regex, datetime, hashlib, base64 modules
// Returns "deterministic" for simple pure functions
func detectCompilationMode(sourceCode string) string {
	// List of imports that require complex mode
	complexImports := []string{
		"import csv",
		"from csv",
		"import io",
		"from io",
		"import re",
		"from re",
		"import datetime",
		"from datetime",
		"import hashlib",
		"from hashlib",
		"import base64",
		"from base64",
		"import json",
		"from json",
		"import uuid",
		"from uuid",
	}

	// Check if any complex imports are present
	for _, imp := range complexImports {
		if strings.Contains(sourceCode, imp) {
			return "complex"
		}
	}

	// Default to deterministic mode for simple functions
	return "deterministic"
}
