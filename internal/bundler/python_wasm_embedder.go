package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// PythonCodeEmbedder handles dynamic embedding of user Python code into WASM modules
type PythonCodeEmbedder struct {
	userCode     string
	metadata     string
	dataSections []DataSection
}

// DataSection represents a data section in the WASM module
type DataSection struct {
	Offset  int
	Size    int
	Type    int // 1=python_code, 2=config, 3=strings, etc.
	Name    string
	Content string
	NamePtr int // Pointer to name in memory
}

// NewPythonCodeEmbedder creates a new code embedder
func NewPythonCodeEmbedder(userCode, metadata string) *PythonCodeEmbedder {
	return &PythonCodeEmbedder{
		userCode: userCode,
		metadata: metadata,
	}
}

// GenerateEmbeddedWAT generates WAT content with embedded user code
func (e *PythonCodeEmbedder) GenerateEmbeddedWAT() (string, error) {
	// Calculate data section layout
	e.calculateDataSections()

	// Generate the data section table
	dataSectionTable := e.generateDataSectionTable()

	// Generate data sections
	dataSections := e.generateDataSections()

	// Generate data section offset globals
	dataSectionOffsets := e.generateDataSectionOffsets()

	// Generate Python execution functions
	pythonFunctions := e.generatePythonFunctions()

	// Generate the complete WAT module
	watTemplate := `
(module
  ;; Memory export (required for all function modules)
  (memory (export "memory") 1)  ;; 64KB pages

  ;; Global variables for memory management and code loading
  (global $initialized (mut i32) (i32.const 0))
  (global $python_code_available (mut i32) (i32.const 0))
  (global $embedded_python_code_ptr (mut i32) (i32.const 0))
  (global $embedded_python_code_len (mut i32) (i32.const 0))
  (global $heap_next (mut i32) (i32.const 2048))

%s
%s
%s
%s

  ;; Initialize function - called once on cold start
  (func $init (export "init")
    ;; Mark as initialized
    i32.const 1
    global.set $initialized

    ;; Initialize embedded Python code
    i32.const %d  ;; offset to main Python code
    i32.const %d  ;; length of Python code
    call $init_python_code
  )

  ;; Execute function - main entry point for function execution
  (func $execute (export "execute") (param $input i32) (param $input_len i32) (result i32)
    ;; Check if initialized
    global.get $initialized
    i32.eqz
    if
      ;; Auto-initialize if not done
      call $init
    end

    ;; Execute Python handler with parameters
    local.get $input
    local.get $input_len
    call $execute_python_handler
    return
  )

  ;; Get metadata function
  (func $metadata (export "metadata") (result i32)
    ;; Return pointer to metadata JSON
    i32.const %d  ;; metadata offset
  )

  ;; Alloc function for memory management (bump allocator within first page)
  (func $alloc (export "alloc") (param $size i32) (result i32)
    (local $ret i32)
    global.get $heap_next
    local.tee $ret
    local.get $size
    i32.add
    global.set $heap_next
    local.get $ret
  )

  ;; Dealloc function (stub)
  (func $dealloc (export "dealloc") (param $ptr i32)
    ;; Stub implementation - would free memory in real allocator
    nop
  )

  ;; _start function - WASI entry point
  (func $_start (export "_start")
    ;; Initialize if needed
    global.get $initialized
    i32.eqz
    if
      call $init
    end
    ;; Return void (no result) - don't call execute here as it returns i32
  )

  ;; main function - alternative entry point
  (func $main (export "main")
    call $_start
  )

  ;; handler function - for runtime that passes input via params
  (func $handler (export "handler") (param $input_ptr i32) (param $input_len i32) (result i32)
    ;; Initialize if needed
    global.get $initialized
    i32.eqz
    if
      call $init
    end
    ;; Call execute with the provided input
    local.get $input_ptr
    local.get $input_len
    call $execute
  )
)`

	// Find main Python code section
	mainCodeOffset := 0
	mainCodeLen := 0
	metadataOffset := 0

	for _, section := range e.dataSections {
		if section.Type == 1 && section.Name == "python_main" {
			mainCodeOffset = section.Offset
			mainCodeLen = section.Size
		} else if section.Type == 2 && section.Name == "metadata" {
			metadataOffset = section.Offset
		}
	}

	watContent := fmt.Sprintf(watTemplate,
		dataSectionTable,
		dataSections,
		dataSectionOffsets,
		pythonFunctions,
		mainCodeOffset,
		mainCodeLen-1, // -1 to exclude null terminator
		metadataOffset)

	return watContent, nil
}

// calculateDataSections computes the layout of data sections in memory
func (e *PythonCodeEmbedder) calculateDataSections() {
	currentOffset := 1024 // Start after some reserved space

	// Add main Python code section
	e.dataSections = append(e.dataSections, DataSection{
		Offset:  currentOffset,
		Size:    len(e.userCode) + 1, // +1 for null terminator
		Type:    1,                   // python_code
		Name:    "python_main",
		Content: e.userCode + "\x00", // null-terminated
	})

	currentOffset += len(e.userCode) + 1

	// Add metadata section
	e.dataSections = append(e.dataSections, DataSection{
		Offset:  currentOffset,
		Size:    len(e.metadata) + 1,
		Type:    2, // config/metadata
		Name:    "metadata",
		Content: e.metadata + "\x00",
	})

	// Add any additional sections as needed
	// Could add strings, config, etc.
}

// packInt32 converts an int32 to little-endian hex escape sequence for WAT
func packInt32(value int) string {
	bytes := make([]byte, 4)
	bytes[0] = byte(value & 0xFF)
	bytes[1] = byte((value >> 8) & 0xFF)
	bytes[2] = byte((value >> 16) & 0xFF)
	bytes[3] = byte((value >> 24) & 0xFF)

	// Convert to hex escape sequences for WAT
	var hexParts []string
	for _, b := range bytes {
		hexParts = append(hexParts, fmt.Sprintf("\\%02x", b))
	}
	return strings.Join(hexParts, "")
}

// CreateEmbeddedPythonWasm creates a WASM module with embedded user Python code
func CreateEmbeddedPythonWasm(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Create metadata JSON
	metadata := fmt.Sprintf(`{
		"name": "%s",
		"runtime": "python-embedded",
		"runtime_version": "micropython-1.20",
		"version": "%s",
		"entry_point": "handler",
		"dependencies": [],
		"memory_mb": 128,
		"timeout_ms": 5000,
		"uses_network": false,
		"uses_filesystem": false,
		"embedded_code_size": %d
	}`, manifest.Name, manifest.Version, len(sourceCode))

	// Create embedder and generate WAT
	embedder := NewPythonCodeEmbedder(sourceCode, metadata)
	watContent, err := embedder.GenerateEmbeddedWAT()
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedded WAT: %v", err)
	}

	// Write WAT to temporary file for compilation
	tempDir := os.TempDir()
	watFile := filepath.Join(tempDir, fmt.Sprintf("embedded-python-%d.wat", os.Getpid()))
	if err := os.WriteFile(watFile, []byte(watContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write WAT file: %v", err)
	}
	defer os.Remove(watFile)

	// Compile WAT to WASM
	wasmBytes, err := compileWATToWasm(watContent)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WAT to WASM: %v", err)
	}

	return wasmBytes, nil
}
