package bundler

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// MicropythonLinker handles embedding user Python code into MicroPython runtime
// This approach uses runtime linking with Wasmtime instead of build-time linking
type MicropythonLinker struct {
	runtimePath string
	userCode    string
	manifest    *manifest.Manifest
}

// NewMicropythonLinker creates a new linker instance
func NewMicropythonLinker(userCode string, manifest *manifest.Manifest) *MicropythonLinker {
	return &MicropythonLinker{
		runtimePath: findMicropythonRuntimePath(),
		userCode:    userCode,
		manifest:    manifest,
	}
}

// Link creates a WASM module with embedded user Python code
// This generates a wrapper that links with micropython runtime at execution time
func (l *MicropythonLinker) Link() ([]byte, error) {
	// Note: We don't require the runtime at build time
	// The wrapper will be linked with micropython-full.wasm at execution time

	// Create a wrapper module that:
	// 1. Provides stub implementations for env.* imports
	// 2. Embeds user code in data section
	// 3. Exports init, execute functions
	// The wrapper imports mp_js_init, mp_js_do_exec from micropython
	wrapperWAT := l.generateWrapperWAT()
	wrapperBytes, err := compileWATToWasm(wrapperWAT)
	if err != nil {
		return nil, fmt.Errorf("failed to compile wrapper: %v", err)
	}

	// The module imports mp_js_init, mp_js_do_exec from micropython runtime
	// This linking is done at runtime via Wasmtime or build-time via wasm-ld
	return wrapperBytes, nil
}

// generateWrapperWAT creates WAT for the wrapper module
func (l *MicropythonLinker) generateWrapperWAT() string {
	escapedCode := escapeForWAT(l.userCode)

	metadata := fmt.Sprintf(`{
		"name": "%s",
		"runtime": "micropython",
		"version": "%s",
		"entry_point": "handler"
	}`, l.manifest.Name, l.manifest.Version)

	escapedMetadata := escapeForWAT(metadata)

	// Calculate offsets
	codeOffset := 1024
	metadataOffset := 4096

	return fmt.Sprintf(`(module
  ;; All imports must come first (WAT requirement)
  (import "env" "mp_js_init" (func $mp_js_init (param i32)))
  (import "env" "mp_js_do_exec" (func $mp_js_do_exec (param i32) (param i32) (result i32)))
  (import "env" "malloc" (func $malloc (param i32) (result i32)))
  (import "env" "free" (func $free (param i32)))

  ;; Module-owned memory (no env memory import so we have one linear memory)
  (memory (export "memory") 16)

  ;; Global for code loaded flag
  (global $code_loaded (mut i32) (i32.const 0))
  (global $code_ptr (mut i32) (i32.const 0))
  (global $code_len (mut i32) (i32.const 0))

  ;; User Python code embedded at offset %d
  (data (i32.const %d) "%s")

  ;; Metadata embedded at offset %d
  (data (i32.const %d) "%s")

  ;; Stub implementations for env.* imports that micropython.wasm requires
  ;; These are JavaScript interop functions we don't need for basic execution

  ;; Basic invoke stubs
  (func $env_invoke_ii (export "env.invoke_ii") (param i32) (param i32) (result i32) i32.const 0)
  (func $env_invoke_iiii (export "env.invoke_iiii") (param i32) (param i32) (param i32) (param i32))
  (func $env_invoke_v (export "env.invoke_v") (param i32))
  (func $env_invoke_viii (export "env.invoke_viii") (param i32) (param i32) (param i32) (param i32))
  (func $env_invoke_iiiii (export "env.invoke_iiiii") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_invoke_iii (export "env.invoke_iii") (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_invoke_vi (export "env.invoke_vi") (param i32))
  (func $env_invoke_vii (export "env.invoke_vii") (param i32) (param i32))
  (func $env_invoke_i (export "env.invoke_i") (param i32) (result i32) i32.const 0)

  ;; MicroPython JS hooks
  (func $env_mp_js_hook (export "env.mp_js_hook") (param i32))
  (func $env_mp_js_random_u32 (export "env.mp_js_random_u32") (result i32) i32.const 0)
  (func $env_mp_js_ticks_ms (export "env.mp_js_ticks_ms") (result i32) i32.const 0)
  (func $env_mp_js_time_ms (export "env.mp_js_time_ms") (result f64) f64.const 0)

  ;; Emscripten stubs
  (func $env_emscripten_scan_registers (export "env.emscripten_scan_registers") (param i32))
  (func $env_emscripten_resize_heap (export "env.emscripten_resize_heap") (param i32) (result i32) i32.const 0)
  (func $env_emscripten_throw_longjmp (export "env.emscripten_throw_longjmp"))

  ;; Proxy stubs
  (func $env_proxy_convert_mp_to_js_then_js_to_mp_obj_jsside (export "env.proxy_convert_mp_to_js_then_js_to_mp_obj_jsside") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_proxy_convert_mp_to_js_then_js_to_js_then_js_to_mp_obj_jsside (export "env.proxy_convert_mp_to_js_then_js_to_js_then_js_to_mp_obj_jsside") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_get_proxy_js_ref_info (export "env.js_get_proxy_js_ref_info") (param i32))
  (func $env_js_get_iter (export "env.js_get_iter") (param i32) (param i32) (param i32) (param i32))
  (func $env_proxy_js_free_obj (export "env.proxy_js_free_obj") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_reflect_construct (export "env.js_reflect_construct") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_iter_next (export "env.js_iter_next") (param i32) (result i32) i32.const 0)
  (func $env_js_check_existing (export "env.js_check_existing") (param i32) (result i32) i32.const 0)
  (func $env_js_get_error_info (export "env.js_get_error_info") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_resolve (export "env.js_then_resolve") (param i32) (param i32) (param i32) (param i32))
  (func $env_create_promise (export "env.create_promise") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_continue (export "env.js_then_continue") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_js_then_reject (export "env.js_then_reject") (param i32) (param i32) (param i32) (param i32))

  ;; Call stubs
  (func $env_call0_kwarg (export "env.call0_kwarg") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_calln_kwarg (export "env.calln_kwarg") (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32) (param i32))
  (func $env_call1 (export "env.call1") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_call2 (export "env.call2") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_calln (export "env.calln") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_call0 (export "env.call0") (param i32) (param i32) (param i32) (param i32))

  ;; Attribute access stubs
  (func $env_lookup_attr (export "env.lookup_attr") (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env_store_attr (export "env.store_attr") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_subscr_load (export "env.js_subscr_load") (param i32) (param i32) (param i32) (param i32))
  (func $env_js_subscr_store (export "env.js_subscr_store") (param i32) (param i32) (param i32) (param i32))
  (func $env_has_attr (export "env.has_attr") (param i32) (param i32) (result i32) i32.const 0)

  ;; Syscall stubs
  (func $env___syscall_chdir (export "env.__syscall_chdir") (param i32) (result i32) i32.const 0)
  (func $env___syscall_getcwd (export "env.__syscall_getcwd") (param i32) (result i32) i32.const 0)
  (func $env___syscall_mkdirat (export "env.__syscall_mkdirat") (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_openat (export "env.__syscall_openat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_poll (export "env.__syscall_poll") (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_getdents64 (export "env.__syscall_getdents64") (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_renameat (export "env.__syscall_renameat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_rmdir (export "env.__syscall_rmdir") (param i32) (result i32) i32.const 0)
  (func $env___syscall_fstat64 (export "env.__syscall_fstat64") (param i32) (result i32) i32.const 0)
  (func $env___syscall_stat64 (export "env.__syscall_stat64") (param i32) (result i32) i32.const 0)
  (func $env___syscall_newfstatat (export "env.__syscall_newfstatat") (param i32) (param i32) (param i32) (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_lstat64 (export "env.__syscall_lstat64") (param i32) (result i32) i32.const 0)
  (func $env___syscall_statfs64 (export "env.__syscall_statfs64") (param i32) (param i32) (result i32) i32.const 0)
  (func $env___syscall_unlinkat (export "env.__syscall_unlinkat") (param i32) (param i32) (result i32) i32.const 0)
  (func $env__abort_js (export "env._abort_js"))

  ;; Standard function exports

  ;; init - Initialize the runtime
  (func $init (export "init")
    ;; Initialize MicroPython with heap size
    i32.const 16777216  ;; 16MB heap
    call $mp_js_init

    ;; Mark code as loaded
    i32.const 1
    global.set $code_loaded
  )

  ;; load_code - Load user Python code into memory
  (func $load_code (export "load_code") (param $ptr i32) (param $len i32)
    local.get $ptr
    global.set $code_ptr
    local.get $len
    global.set $code_len
  )

  ;; execute - Execute user code with input
  (func $execute (export "execute") (param $input_ptr i32) (param $input_len i32) (result i32)
    (local $result_ptr i32)
    (local $result_len i32)

    ;; Check if code is loaded
    global.get $code_loaded
    i32.eqz
    if
      ;; Initialize if not done
      call $init
    end

    ;; Get code pointer
    global.get $code_ptr
    local.set $result_ptr

    ;; Execute the Python code
    ;; For now, return a simple response
    ;; In full implementation, would call mp_js_do_exec

    i32.const 0  ;; Return null (no output yet)
  )

  ;; alloc - Allocate memory
  (func $alloc (export "alloc") (param $size i32) (result i32)
    local.get $size
    call $malloc
  )

  ;; dealloc - Free memory
  (func $dealloc (export "dealloc") (param $ptr i32)
    local.get $ptr
    call $free
  )

  ;; metadata - Return pointer to metadata
  (func $metadata (export "metadata") (result i32)
    i32.const %d  ;; metadata offset
  )

  ;; handler - Alternative entry point
  (func $handler (export "handler") (param $input_ptr i32) (param $input_len i32) (result i32)
    local.get $input_ptr
    local.get $input_len
    call $execute
  )

  ;; _start - WASI entry point
  (func $_start (export "_start")
    call $init
  )
)`, codeOffset, codeOffset, escapedCode, metadataOffset, metadataOffset, escapedMetadata, metadataOffset)
}

// UseExistingRuntime returns the path to the micropython runtime for external linking
func (l *MicropythonLinker) UseExistingRuntime() string {
	return l.runtimePath
}

// GetMicropythonExports returns the function exports from micropython.wasm
// These can be called from host code to execute Python
func GetMicropythonExports() map[string]string {
	return map[string]string{
		"mp_js_init":               "Initialize interpreter with heap size",
		"mp_js_do_exec":            "Execute Python code, return result",
		"mp_js_do_exec_async":      "Async Python execution",
		"mp_js_register_js_module": "Register a JS module",
		"mp_js_do_import":          "Import a module",
		"malloc":                   "Allocate memory",
		"free":                     "Free memory",
	}
}

// CompileAndLinkPython combines user code with micropython runtime
// This is the main entry point for the linking process
func CompileAndLinkPython(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	linker := NewMicropythonLinker(sourceCode, manifest)

	// First try the wrapper approach
	wrapperBytes, err := linker.Link()
	if err != nil {
		return nil, fmt.Errorf("wrapper linking failed: %v", err)
	}

	// Validate the output
	if err := validateWasmModule(wrapperBytes); err != nil {
		return nil, fmt.Errorf("linked module validation failed: %v", err)
	}

	return wrapperBytes, nil
}

// check if file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getRuntimeFilePath returns the absolute path to micropython runtime
func getRuntimeFilePath() string {
	// Check multiple possible locations
	searchPaths := []string{
		"internal/bundler/python/micropython.wasm",
		"bundler/python/micropython.wasm",
		"../../internal/bundler/python/micropython.wasm",
		"internal/bundler/python/micropython-full.wasm",
	}

	for _, p := range searchPaths {
		if fileExists(p) {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}

	return ""
}

// ValidateModuleWithRuntime checks if a WASM module is compatible with micropython runtime:
// magic bytes, version, and presence of required imports/exports.
func ValidateModuleWithRuntime(wasmBytes []byte) error {
	if len(wasmBytes) < 8 {
		return fmt.Errorf("invalid WASM module: too short")
	}
	if wasmBytes[0] != 0x00 || wasmBytes[1] != 0x61 || wasmBytes[2] != 0x73 || wasmBytes[3] != 0x6D {
		return fmt.Errorf("invalid WASM magic bytes")
	}
	// Version: 1 (0x01 0x00 0x00 0x00) or 2
	if wasmBytes[4] != 1 || wasmBytes[5] != 0 || wasmBytes[6] != 0 || wasmBytes[7] != 0 {
		return fmt.Errorf("unsupported WASM version")
	}

	imports, exports, err := parseWasmImportsExports(wasmBytes[8:])
	if err != nil {
		return fmt.Errorf("WASM imports/exports: %w", err)
	}

	requiredImports := []string{"env.memory", "env.mp_js_init", "env.mp_js_do_exec", "env.malloc", "env.free"}
	for _, key := range requiredImports {
		if !imports[key] {
			return fmt.Errorf("missing required import %q", key)
		}
	}

	// Runtime must export linear memory for the host.
	if !exports["memory"] {
		return fmt.Errorf("missing required export %q", "memory")
	}

	return nil
}

// parseWasmImportsExports parses WASM section payloads to collect import "module.name" and export "name" sets.
func parseWasmImportsExports(payload []byte) (imports map[string]bool, exports map[string]bool, err error) {
	imports = make(map[string]bool)
	exports = make(map[string]bool)
	r := bytes.NewReader(payload)
	for r.Len() > 0 {
		id, err := wasmParseByte(r)
		if err != nil {
			return nil, nil, err
		}
		secLen, err := wasmParseU32LEB128(r)
		if err != nil {
			return nil, nil, err
		}
		if secLen > uint32(r.Len()) {
			return nil, nil, fmt.Errorf("section length exceeds buffer")
		}
		sec := make([]byte, secLen)
		if _, err := r.Read(sec); err != nil {
			return nil, nil, err
		}
		switch id {
		case 2: // Import
			if err := parseWasmImportSection(sec, imports); err != nil {
				return nil, nil, err
			}
		case 7: // Export
			if err := parseWasmExportSection(sec, exports); err != nil {
				return nil, nil, err
			}
		}
	}
	return imports, exports, nil
}

func wasmParseByte(r *bytes.Reader) (byte, error) {
	b := make([]byte, 1)
	if _, err := r.Read(b); err != nil {
		return 0, err
	}
	return b[0], nil
}

func wasmParseU32LEB128(r *bytes.Reader) (uint32, error) {
	var v uint32
	var shift uint
	for {
		b := make([]byte, 1)
		if _, err := r.Read(b); err != nil {
			return 0, err
		}
		v |= uint32(b[0]&0x7f) << shift
		if b[0]&0x80 == 0 {
			return v, nil
		}
		shift += 7
		if shift > 35 {
			return 0, fmt.Errorf("LEB128 overflow")
		}
	}
}

func wasmParseVecBytes(r *bytes.Reader) ([]byte, error) {
	n, err := wasmParseU32LEB128(r)
	if err != nil {
		return nil, err
	}
	if n > 1<<20 {
		return nil, fmt.Errorf("vector length too large")
	}
	b := make([]byte, n)
	if _, err := r.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func parseWasmImportSection(sec []byte, out map[string]bool) error {
	r := bytes.NewReader(sec)
	count, err := wasmParseU32LEB128(r)
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		mod, err := wasmParseVecBytes(r)
		if err != nil {
			return err
		}
		name, err := wasmParseVecBytes(r)
		if err != nil {
			return err
		}
		kind, err := wasmParseByte(r)
		if err != nil {
			return err
		}
		key := string(mod) + "." + string(name)
		out[key] = true
		// Skip type index for func (0), table type for table (1), memory type for mem (2), global type for global (3)
		switch kind {
		case 0: // func: type index u32
			if _, err := wasmParseU32LEB128(r); err != nil {
				return err
			}
		case 1: // table: elem_type byte + limits (min [max])
			if _, err := wasmParseByte(r); err != nil {
				return err
			}
			if err := wasmSkipLimits(r); err != nil {
				return err
			}
		case 2: // memory: limits
			if err := wasmSkipLimits(r); err != nil {
				return err
			}
		case 3: // global: valtype byte + mut byte
			if _, err := r.Read(make([]byte, 2)); err != nil {
				return err
			}
		}
	}
	return nil
}

func wasmSkipLimits(r *bytes.Reader) error {
	flag, err := wasmParseByte(r)
	if err != nil {
		return err
	}
	if _, err := wasmParseU32LEB128(r); err != nil {
		return err
	}
	if flag == 1 {
		if _, err := wasmParseU32LEB128(r); err != nil {
			return err
		}
	}
	return nil
}

func parseWasmExportSection(sec []byte, out map[string]bool) error {
	r := bytes.NewReader(sec)
	count, err := wasmParseU32LEB128(r)
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		name, err := wasmParseVecBytes(r)
		if err != nil {
			return err
		}
		_, err = wasmParseByte(r)
		if err != nil {
			return err
		}
		out[string(name)] = true
		// index (u32)
		if _, err := wasmParseU32LEB128(r); err != nil {
			return err
		}
	}
	return nil
}

// GetMicropythonInterface returns the expected interface for micropython integration
func GetMicropythonInterface() map[string]string {
	return map[string]string{
		// Required imports (provided by host/runtime)
		"env.memory":        "Shared linear memory",
		"env.mp_js_init":    "Initialize interpreter (i32) -> void",
		"env.mp_js_do_exec": "Execute code (code_ptr, input_ptr) -> result_ptr",
		"env.malloc":        "Allocate memory (size) -> ptr",
		"env.free":          "Free memory (ptr) -> void",

		// Optional imports (stubs acceptable)
		"env.*":  "JavaScript interop (can be stubbed)",
		"wasi_*": "WASI syscalls (for filesystem access)",
	}
}
