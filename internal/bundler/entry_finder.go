package bundler

import (
	"path/filepath"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// findEntryFile locates the entry file based on manifest configuration and available files.
// It handles working directory resolution and provides detailed error information.
func findEntryFile(manifest *manifest.Manifest) (string, error) {
	// First check if entry is explicitly specified in manifest
	if manifest.Entry != "" {
		// Resolve the entry file path relative to working directory if needed
		entryPath := manifest.Entry
		if !filepath.IsAbs(entryPath) {
			// Assume it's relative to the project root/working directory
			entryPath = filepath.Clean(entryPath)
		}

		if err := ValidateEntryFile(entryPath); err != nil {
			return "", &EntryFileInvalidError{
				Path:   manifest.Entry,
				Reason: err.Error(),
			}
		}
		return entryPath, nil
	}

	// Fall back to auto-detection based on runtime
	preferred, alternatives := getEntryFileCandidates(manifest.Runtime)

	// Check if preferred entry file exists
	if err := ValidateEntryFile(preferred); err == nil {
		return preferred, nil
	}

	// Try alternative extensions based on runtime
	for _, alt := range alternatives {
		if err := ValidateEntryFile(alt); err == nil {
			return alt, nil
		}
	}

	return "", &EntryFileNotFoundError{
		Runtime:      manifest.Runtime,
		Preferred:    preferred,
		Alternatives: alternatives,
	}
}

// getEntryFileCandidates returns the preferred entry file and alternatives for a given runtime
func getEntryFileCandidates(runtime string) (preferred string, alternatives []string) {
	switch runtime {
	case "deno":
		return "main.ts", []string{"index.ts", "main.js", "index.js"}
	case "python3.11":
		return "main.py", []string{"index.py", "app.py", "__main__.py"}
	case "go":
		return "main.go", []string{"handler.go", "index.go"}
	case "c":
		return "main.c", []string{"index.c", "handler.c"}
	case "kotlin":
		return "Main.kt", []string{"main.kt", "Handler.kt", "App.kt"}
	case "ruby":
		return "main.rb", []string{"index.rb", "app.rb", "handler.rb"}
	case "swift":
		return "main.swift", []string{"Sources/main.swift", "Handler.swift", "App.swift"}
	case "microvm":
		return "rootfs.tar", []string{"rootfs.squashfs", "rootfs.img", "Dockerfile"}
	case "wasm":
		return "index.wasm", []string{"main.wasm", "index.wat", "main.wat"}
	case "prism":
		return "index.js", []string{"main.js", "index.ts", "main.ts", "main.py", "handler.go"}
	default: // node18, node20, and other JS runtimes
		return "index.js", []string{"index.ts", "main.js", "main.ts"}
	}
}
