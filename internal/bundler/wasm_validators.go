package bundler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// WASMValidationConfig contains configuration for WASM validation
type WASMValidationConfig struct {
	// MaxBinarySize is the maximum allowed WASM binary size in bytes
	MaxBinarySize uint32

	// RequireMemoryExport requires the module to export memory
	RequireMemoryExport bool

	// AllowWASI indicates whether WASI imports are allowed
	AllowWASI bool

	// BlockedImports are import paths that are not allowed
	BlockedImports []string

	// MaxImports is the maximum number of imports allowed
	MaxImports uint32

	// MaxExports is the maximum number of exports allowed
	MaxExports uint32

	// MaxFunctions is the maximum number of functions allowed
	MaxFunctions uint32

	// MaxMemoryPages is the maximum memory pages (65536 bytes each)
	MaxMemoryPages uint32

	// EnableStrictValidation enables strict validation rules
	EnableStrictValidation bool
}

// DefaultWASMValidationConfig returns a default validation config
func DefaultWASMValidationConfig() *WASMValidationConfig {
	return &WASMValidationConfig{
		MaxBinarySize:          10 * 1024 * 1024, // 10MB
		RequireMemoryExport:    false,
		AllowWASI:              true,
		BlockedImports:         []string{},
		MaxImports:             100,
		MaxExports:             200,
		MaxFunctions:           500,
		MaxMemoryPages:         1024, // 64MB
		EnableStrictValidation: false,
	}
}

// validateWasmModule performs comprehensive validation on compiled WebAssembly bytes
func validateWasmModule(wasmBytes []byte) error {
	return ValidateWASM(wasmBytes, DefaultWASMValidationConfig())
}

// ValidateWASM performs comprehensive WASM validation with custom config
func ValidateWASM(wasmBytes []byte, config *WASMValidationConfig) error {
	if config == nil {
		config = DefaultWASMValidationConfig()
	}

	// Check minimum size
	if len(wasmBytes) < 8 {
		return fmt.Errorf("WebAssembly module too small: %d bytes", len(wasmBytes))
	}

	// Check maximum size
	if uint32(len(wasmBytes)) > config.MaxBinarySize {
		return fmt.Errorf("WebAssembly module too large: %d bytes (max: %d)", len(wasmBytes), config.MaxBinarySize)
	}

	// Check magic bytes (\0asm)
	if string(wasmBytes[0:4]) != "\x00asm" {
		return fmt.Errorf("invalid WebAssembly magic bytes: %x", wasmBytes[0:4])
	}

	// Check version (should be 1 or 2 for MVP and stable)
	version := binary.LittleEndian.Uint32(wasmBytes[4:8])
	if version != 1 && version != 2 {
		return fmt.Errorf("unsupported WebAssembly version: %d", version)
	}

	// Parse and validate sections
	reader := bytes.NewReader(wasmBytes[8:])
	sectionCount := 0

	for reader.Len() > 0 {
		sectionID, err := reader.ReadByte()
		if err != nil {
			break // End of sections
		}

		// Read section size (varint7 can be up to 4 bytes)
		sectionSize, _, err := readVarUint32(reader)
		if err != nil {
			return fmt.Errorf("failed to read section size: %w", err)
		}

		sectionStart := reader.Len()
		sectionCount++

		// Validate by section type
		switch sectionID {
		case 0: // Custom section
			// Skip custom sections

		case 1: // Type section
			typeCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read type count: %w", err)
			}
			if typeCount > 1000 {
				return fmt.Errorf("too many types: %d (max: 1000)", typeCount)
			}

		case 2: // Import section
			importCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read import count: %w", err)
			}
			if importCount > config.MaxImports {
				return fmt.Errorf("too many imports: %d (max: %d)", importCount, config.MaxImports)
			}

			for i := uint32(0); i < importCount; i++ {
				// Validate import module and field
				moduleLen, _, err := readVarUint32(reader)
				if err != nil {
					return fmt.Errorf("failed to read import module length: %w", err)
				}
				if moduleLen > 100 {
					return fmt.Errorf("import module name too long: %d", moduleLen)
				}
				moduleBytes := make([]byte, moduleLen)
				reader.Read(moduleBytes) //nolint:errcheck

				fieldLen, _, err := readVarUint32(reader)
				if err != nil {
					return fmt.Errorf("failed to read import field length: %w", err)
				}
				if fieldLen > 100 {
					return fmt.Errorf("import field name too long: %d", fieldLen)
				}
				fieldBytes := make([]byte, fieldLen)
				reader.Read(fieldBytes) //nolint:errcheck

				// Read import kind
				kind, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("failed to read import kind: %w", err)
				}

				// Check blocked imports
				moduleStr := string(moduleBytes)
				fieldStr := string(fieldBytes)
				for _, blocked := range config.BlockedImports {
					if strings.HasPrefix(blocked, moduleStr+":") && strings.TrimPrefix(blocked, moduleStr+":") == fieldStr {
						return fmt.Errorf("blocked import: %s", blocked)
					}
					if blocked == moduleStr+"::*" {
						return fmt.Errorf("blocked import module: %s", moduleStr)
					}
				}

				// Validate import kind
				switch kind {
				case 0: // Function
					typeIndex, _, err := readVarUint32(reader)
					if err != nil {
						return fmt.Errorf("failed to read function type index: %w", err)
					}
					_ = typeIndex
				case 1: // Table
					return fmt.Errorf("tables are not supported")
				case 2: // Memory
					// Validate memory type
					if err := validateMemoryType(reader); err != nil {
						return fmt.Errorf("invalid memory import: %w", err)
					}
				case 3: // Global
					return fmt.Errorf("globals are not supported in imports")
				default:
					return fmt.Errorf("unknown import kind: %d", kind)
				}
			}

		case 3: // Function section
			functionCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read function count: %w", err)
			}
			if functionCount > config.MaxFunctions {
				return fmt.Errorf("too many functions: %d (max: %d)", functionCount, config.MaxFunctions)
			}

		case 4: // Table section
			tableCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read table count: %w", err)
			}
			if tableCount > 0 {
				return fmt.Errorf("tables are not supported")
			}

		case 5: // Memory section
			memoryCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read memory count: %w", err)
			}
			if memoryCount > 1 {
				return fmt.Errorf("too many memory sections: %d", memoryCount)
			}

			for i := uint32(0); i < memoryCount; i++ {
				if err := validateMemoryType(reader); err != nil {
					return fmt.Errorf("invalid memory: %w", err)
				}
			}

		case 6: // Global section
			globalCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read global count: %w", err)
			}
			if globalCount > 0 && config.EnableStrictValidation {
				return fmt.Errorf("globals are not supported")
			}

		case 7: // Export section
			exportCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read export count: %w", err)
			}
			if exportCount > config.MaxExports {
				return fmt.Errorf("too many exports: %d (max: %d)", exportCount, config.MaxExports)
			}

			for i := uint32(0); i < exportCount; i++ {
				nameLen, _, err := readVarUint32(reader)
				if err != nil {
					return fmt.Errorf("failed to read export name length: %w", err)
				}
				if nameLen > 100 {
					return fmt.Errorf("export name too long: %d", nameLen)
				}
				nameBytes := make([]byte, nameLen)
				reader.Read(nameBytes) //nolint:errcheck

				kind, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("failed to read export kind: %w", err)
				}

				// Skip index
				switch kind {
				case 0, 1, 2, 3: // Function, Table, Memory, Global
					if _, _, err := readVarUint32(reader); err != nil {
						return fmt.Errorf("failed to read export index: %w", err)
					}
				default:
					return fmt.Errorf("unknown export kind: %d", kind)
				}
			}

		case 8: // Start section
			_, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read start function index: %w", err)
			}

		case 9: // Element section
			elemCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read element count: %w", err)
			}
			if elemCount > 0 && config.EnableStrictValidation {
				return fmt.Errorf("table elements are not supported")
			}

		case 10: // Code section
			codeCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read code count: %w", err)
			}

			for i := uint32(0); i < codeCount; i++ {
				codeSize, _, err := readVarUint32(reader)
				if err != nil {
					return fmt.Errorf("failed to read code size: %w", err)
				}
				if codeSize > 1024*1024 { // 1MB per function
					return fmt.Errorf("function code too large: %d bytes", codeSize)
				}
				// Skip code body
				_, err = reader.Seek(int64(codeSize), 1)
				if err != nil {
					return fmt.Errorf("failed to skip code body: %w", err)
				}
			}

		case 11: // Data section
			dataCount, _, err := readVarUint32(reader)
			if err != nil {
				return fmt.Errorf("failed to read data count: %w", err)
			}
			if dataCount > 0 && config.EnableStrictValidation {
				return fmt.Errorf("data sections are not supported")
			}

		default:
			// Unknown section, skip it
		}

		// Verify we read the correct number of bytes
		bytesRead := sectionStart - reader.Len()
		if int64(bytesRead) != int64(sectionSize) {
			return fmt.Errorf("section %d size mismatch: expected %d, read %d", sectionID, sectionSize, bytesRead)
		}
	}

	return nil
}

// validateMemoryType validates a memory type from a reader
func validateMemoryType(reader *bytes.Reader) error {
	// Memory type is (limits)
	memType, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read memory type: %w", err)
	}

	if memType != 0x00 { // Only 0x00 (resizable_limits) is valid for MVP
		return fmt.Errorf("invalid memory type: %x", memType)
	}

	flags, _, err := readVarUint32(reader)
	if err != nil {
		return fmt.Errorf("failed to read memory flags: %w", err)
	}

	initialPages, _, err := readVarUint32(reader)
	if err != nil {
		return fmt.Errorf("failed to read initial memory pages: %w", err)
	}

	// Maximum pages is 65536 (4GB) for WASM MVP
	if initialPages > 65536 {
		return fmt.Errorf("initial memory pages too large: %d", initialPages)
	}

	// If bit 0 is set, there's a maximum
	if flags&1 != 0 {
		maxPages, _, err := readVarUint32(reader)
		if err != nil {
			return fmt.Errorf("failed to read maximum memory pages: %w", err)
		}
		if maxPages < initialPages {
			return fmt.Errorf("maximum memory pages less than initial: %d < %d", maxPages, initialPages)
		}
		if maxPages > 65536 {
			return fmt.Errorf("maximum memory pages too large: %d", maxPages)
		}
	}

	return nil
}

// readVarUint32 reads a variable-length unsigned 32-bit integer
func readVarUint32(reader *bytes.Reader) (uint32, int, error) {
	var result uint32
	var shift uint
	var bytesRead int

	for shift < 32 {
		b, err := reader.ReadByte()
		if err != nil {
			return 0, bytesRead, err
		}
		bytesRead++

		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, bytesRead, nil
		}
		shift += 7
	}

	return 0, bytesRead, fmt.Errorf("varuint32 too large")
}
