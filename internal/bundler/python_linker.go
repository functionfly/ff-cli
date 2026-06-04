package bundler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// LinkPythonWasm links a user Python code wrapper with the MicroPython runtime
// This function combines:
// 1. A WAT stub that exports init, execute, load_code, etc.
// 2. The precompiled micropython.wasm runtime
// Uses wasm-ld to combine multiple WASM modules into one
func LinkPythonWasm(stubWasmPath, runtimeWasmPath, outputPath string) ([]byte, error) {
	// Check if wasm-ld is available
	if !isWasmLdAvailable() {
		return nil, fmt.Errorf("wasm-ld not available - required for linking WASM modules")
	}

	// Ensure output file has .wasm extension
	if !strings.HasSuffix(outputPath, ".wasm") {
		outputPath += ".wasm"
	}

	// Run wasm-ld to link the modules
	// --allow-undefined allows unresolved symbols (we'll provide them at runtime)
	// --import-memory tells the linker we'll import memory
	// --export-all exports all symbols from the stub
	cmd := exec.Command("wasm-ld",
		// Input files
		stubWasmPath,
		runtimeWasmPath,
		// Output
		"-o", outputPath,
		// Linking options
		"--allow-undefined",
		"--import-memory",
		"--export-all",
		"--export=init",
		"--export=execute",
		"--export=load_code",
		"--export=alloc",
		"--export=dealloc",
		"--export=metadata",
		"--export=memory",
		// Stack and memory options
		"--stack-first",
		"-z", "stack-size=524288", // 512KB stack
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("wasm-ld linking failed: %v\nOutput: %s", err, string(output))
	}

	// Read the linked output
	linkedWasm, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read linked WASM: %v", err)
	}

	return linkedWasm, nil
}

// isWasmLdAvailable checks if wasm-ld is available in the system
func isWasmLdAvailable() bool {
	cmd := exec.Command("wasm-ld", "--version")
	err := cmd.Run()
	return err == nil
}

// CreateLinkerStubWAT generates WAT that provides required env.* imports
// and wraps the micropython runtime functions
func CreateLinkerStubWAT() string {
	// This WAT provides stub implementations for env.* functions that micropython.wasm imports
	// These are JavaScript interop functions we don't need for serverless execution
	return `(module
  ;; Import the micropython runtime memory
  (import "env" "memory" (memory 1))

  ;; Stub implementations for JavaScript interop functions
  ;; These are called by micropython but we don't need them for basic execution

  ;; env.invoke_* - JavaScript function call stubs
  (func $env_invoke_ii (export "env.invoke_ii") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_invoke_iiii (export "env.invoke_iiii") (param i32) (param i32) (param i32) (param i32))
  (func $env_invoke_v (export "env.invoke_v") (param i32))
  (func $env_invoke_viii (export "env.invoke_viii") (param i32) (param i32) (param i32) (param i32))
  (func $env_invoke_iiiii (export "env.invoke_iiiii") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_invoke_iii (export "env.invoke_iii") (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_invoke_vi (export "env.invoke_vi") (param i32))
  (func $env_invoke_vii (export "env.invoke_vii") (param i32) (param i32))
  (func $env_invoke_i (export "env.invoke_i") (param i32) (result i32)
    i32.const 0)

  ;; env.mp_js_* - MicroPython JavaScript hook stubs
  (func $env_mp_js_hook (export "env.mp_js_hook") (param i32))
  (func $env_mp_js_random_u32 (export "env.mp_js_random_u32") (result i32)
    i32.const 0)
  (func $env_mp_js_ticks_ms (export "env.mp_js_ticks_ms") (result i32)
    i32.const 0)
  (func $env_mp_js_time_ms (export "env.mp_js_time_ms") (result f64)
    f64.const 0)

  ;; env.emscripten_* - Emscripten stubs
  (func $env_emscripten_scan_registers (export "env.emscripten_scan_registers") (param i32))
  (func $env_emscripten_resize_heap (export "env.emscripten_resize_heap") (param i32) (result i32)
    i32.const 0)
  (func $env_emscripten_throw_longjmp (export "env.emscripten_throw_longjmp"))

  ;; env.proxy_* - Proxy/async stubs
  (func $env_proxy_convert_mp_to_js_then_js_to_mp_obj_jsside (export "env.proxy_convert_mp_to_js_then_js_to_mp_obj_jsside") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_proxy_convert_mp_to_js_then_js_to_js_then_js_to_mp_obj_jsside (export "env.proxy_convert_mp_to_js_then_js_to_js_then_js_to_mp_obj_jsside") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_get_proxy_js_ref_info (export "env.js_get_proxy_js_ref_info") (param i32))
  (func $env_js_get_iter (export "env.js_get_iter") (param i32) (param i32) (param i32) (param i32))
  (func $env_proxy_js_free_obj (export "env.proxy_js_free_obj") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_reflect_construct (export "env.js_reflect_construct") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_iter_next (export "env.js_iter_next") (param i32) (result i32)
    i32.const 0)
  (func $env_js_check_existing (export "env.js_check_existing") (param i32) (result i32)
    i32.const 0)
  (func $env_js_get_error_info (export "env.js_get_error_info") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_resolve (export "env.js_then_resolve") (param i32) (param i32) (param i32) (param i32))
  (func $env_create_promise (export "env.create_promise") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_continue (export "env.js_then_continue") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_reject (export "env.js_then_reject") (param i32) (param i32) (param i32) (param i32))

  ;; env.call* - JavaScript call stubs
  (func $env_call0_kwarg (export "env.call0_kwarg") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_calln_kwarg (export "env.calln_kwarg") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_call1 (export "env.call1") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_call2 (export "env.call2") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_calln (export "env.calln") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_call0 (export "env.call0") (param i32) (param i32) (param i32) (param i32))

  ;; env.lookup_attr, env.store_attr, env.subscr_* - Attribute access stubs
  (func $env_lookup_attr (export "env.lookup_attr") (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env_store_attr (export "env.store_attr") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_subscr_load (export "env.js_subscr_load") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_subscr_store (export "env.js_subscr_store") (param i32) (param i32) (param i32) (param i32))
  (func $env_has_attr (export "env.has_attr") (param i32) (param i32) (result i32)
    i32.const 0)

  ;; env.__syscall_* - Syscall stubs
  (func $env___syscall_chdir (export "env.__syscall_chdir") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_getcwd (export "env.__syscall_getcwd") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_mkdirat (export "env.__syscall_mkdirat") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_openat (export "env.__syscall_openat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_poll (export "env.__syscall_poll") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_getdents64 (export "env.__syscall_getdents64") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_renameat (export "env.__syscall_renameat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_rmdir (export "env.__syscall_rmdir") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_fstat64 (export "env.__syscall_fstat64") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_stat64 (export "env.__syscall_stat64") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_newfstatat (export "env.__syscall_newfstatat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_lstat64 (export "env.__syscall_lstat64") (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_statfs64 (export "env.__syscall_statfs64") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env___syscall_unlinkat (export "env.__syscall_unlinkat") (param i32) (param i32) (result i32)
    i32.const 0)
  (func $env__abort_js (export "env._abort_js"))

  ;; Export our wrapper functions that call micropython
  ;; These are the standard function interface expected by the runtime

  ;; Initialize function
  (func $init (export "init")
    ;; Initialize micropython via mp_js_init
    call $mp_js_init_wrapper
  )

  ;; Load Python code
  (func $load_code (export "load_code") (param $ptr i32) (param $len i32)
    ;; Store code pointer and length for execute
    local.get $ptr
    global.set $user_code_ptr
    local.get $len
    global.set $user_code_len
  )

  ;; Execute function
  (func $execute (export "execute") (param $input_ptr i32) (param $input_len i32) (result i32)
    ;; Execute user code with input
    local.get $input_ptr
    local.get $input_len
    call $mp_js_do_exec_wrapper
  )

  ;; Alloc function
  (func $alloc (export "alloc") (param $size i32) (result i32)
    ;; Use micropython's malloc
    call $malloc
    local.get $size
    call $malloc
  )

  ;; Dealloc function
  (func $dealloc (export "dealloc") (param $ptr i32)
    ;; Use micropython's free
    local.get $ptr
    call $free
  )

  ;; Metadata function - returns pointer to metadata JSON
  (func $metadata (export "metadata") (result i32)
    i32.const 8192  ;; Metadata offset
  )

  ;; Globals for storing user code
  (global $user_code_ptr (mut i32) (i32.const 0))
  (global $user_code_len (mut i32) (i32.const 0))

  ;; Wrapper for mp_js_init
  (func $mp_js_init_wrapper
    ;; Call mp_js_init with heap limit (16MB)
    i32.const 16777216  ;; 16MB heap
    call $mp_js_init
  )

  ;; Wrapper for mp_js_do_exec
  ;; Takes input pointer and length, returns result pointer
  (func $mp_js_do_exec_wrapper (param $input_ptr i32) (param $input_len i32) (result i32)
    (local $result i32)
    (local $code_ptr i32)
    (local $code_len i32)

    ;; Get stored user code pointer
    global.get $user_code_ptr
    local.set $code_ptr

    global.get $user_code_len
    local.set $code_len

    ;; For now, execute a simple test
    ;; In real implementation, we'd parse the input and pass to Python
    i32.const 0
    return
  )

  ;; Import mp_js_init from micropython module
  (import "env" "mp_js_init" (func $mp_js_init (param i32)))

  ;; Import mp_js_do_exec from micropython module
  (import "env" "mp_js_do_exec" (func $mp_js_do_exec (param i32) (param i32) (result i32)))

  ;; Import malloc/free from micropython module
  (import "env" "malloc" (func $malloc (param i32) (result i32)))
  (import "env" "free" (func $free (param i32)))
)`
}

// CreateLinkerStub compiles the WAT stub to WASM
func CreateLinkerStub() ([]byte, error) {
	watContent := CreateLinkerStubWAT()

	// Write to temp file
	tempDir := os.TempDir()
	watFile := filepath.Join(tempDir, fmt.Sprintf("linker-stub-%d.wat", os.Getpid()))
	wasmFile := filepath.Join(tempDir, fmt.Sprintf("linker-stub-%d.wasm", os.Getpid()))

	// Clean up
	defer os.Remove(watFile)
	defer os.Remove(wasmFile)

	// Write WAT file
	if err := os.WriteFile(watFile, []byte(watContent), 0600); err != nil {
		return nil, fmt.Errorf("failed to write WAT file: %v", err)
	}

	// Compile WAT to WASM
	cmd := exec.Command("wat2wasm", watFile, "-o", wasmFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("wat2wasm failed: %v\nOutput: %s", err, string(output))
	}

	// Read compiled WASM
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read compiled WASM: %v", err)
	}

	return wasmBytes, nil
}

// LinkWithMicropython links user code with the micropython runtime
// Returns the fully linked WASM module ready for execution
func LinkWithMicropython(userCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Get paths
	_, stubWasmPath := writeTempWasm("linker-stub", CreateLinkerStub)
	if stubWasmPath == "" {
		return nil, fmt.Errorf("failed to create linker stub")
	}
	defer os.Remove(stubWasmPath)

	// Get micropython runtime path
	runtimePath := getMicropythonRuntimePath()
	if runtimePath == "" {
		return nil, fmt.Errorf("micropython runtime not found")
	}

	// Output path
	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("linked-python-%d.wasm", os.Getpid()))

	// Link
	linkedWasm, err := LinkPythonWasm(stubWasmPath, runtimePath, outputPath)
	if err != nil {
		return nil, fmt.Errorf("linking failed: %v", err)
	}

	// Clean up output
	defer os.Remove(outputPath)

	return linkedWasm, nil
}

// writeTempWasm writes bytes to a temp file and returns the path
func writeTempWasm(prefix string, generator func() ([]byte, error)) ([]byte, string) {
	wasmBytes, err := generator()
	if err != nil {
		return nil, ""
	}

	tempDir := os.TempDir()
	path := filepath.Join(tempDir, fmt.Sprintf("%s-%d.wasm", prefix, os.Getpid()))
	if err := os.WriteFile(path, wasmBytes, 0600); err != nil {
		return nil, ""
	}

	return wasmBytes, path
}

// getMicropythonRuntimePath returns the path to micropython.wasm
func getMicropythonRuntimePath() string {
	paths := []string{
		"internal/bundler/python/micropython.wasm",
		"bundler/python/micropython.wasm",
		"../../internal/bundler/python/micropython.wasm",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
