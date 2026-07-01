package bundler

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// bundleNative packages source files for runtimes that compile server-side.
// Used by: go, c, kotlin, ruby, swift, microvm.
// No local compilation — files are uploaded as a tar archive.
func bundleNative(m *manifest.Manifest) ([]byte, error) {
	entry, err := findEntryFile(m)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add the manifest
	manifestData, err := os.ReadFile("functionfly.jsonc")
	if err != nil {
		manifestData, err = os.ReadFile("functionfly.json")
	}
	if err == nil {
		if err := addFileToTar(tw, "functionfly.jsonc", manifestData); err != nil {
			return nil, err
		}
	}

	// Add entry file
	entryData, err := os.ReadFile(entry)
	if err != nil {
		return nil, fmt.Errorf("could not read entry file %s: %w", entry, err)
	}
	if err := addFileToTar(tw, entry, entryData); err != nil {
		return nil, err
	}

	// Add common companion files based on runtime
	companions := nativeCompanionFiles(m.Runtime)
	for _, name := range companions {
		if name == entry {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			continue // optional companion
		}
		if err := addFileToTar(tw, name, data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize archive: %w", err)
	}

	return buf.Bytes(), nil
}

// nativeCompanionFiles returns files commonly needed alongside the entry point
// for native-compilation runtimes.
func nativeCompanionFiles(runtime string) []string {
	switch runtime {
	case "go":
		return []string{"go.mod", "go.sum", "handler.go", "main.go"}
	case "c":
		return []string{"Makefile", "CMakeLists.txt", "header.h"}
	case "kotlin":
		return []string{"build.gradle.kts", "build.gradle", "settings.gradle.kts"}
	case "ruby":
		return []string{"Gemfile", "Gemfile.lock", "Rakefile"}
	case "swift":
		return []string{"Package.swift", "Package.resolved"}
	case "microvm":
		return []string{"Dockerfile", "rootfs.tar", "config.json"}
	default:
		return nil
	}
}

func addFileToTar(tw *tar.Writer, name string, data []byte) error {
	// Normalize path separators
	name = strings.ReplaceAll(name, "\\", "/")
	hdr := &tar.Header{
		Name: name,
		Mode: 0600,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := io.Copy(tw, bytes.NewReader(data))
	return err
}

// collectSourceFiles walks the current directory and returns source file paths
// matching the given extensions.
func collectSourceFiles(extensions []string) ([]string, error) {
	var files []string
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet[e] = true
	}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || name == "vendor" || name == "target" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if extSet[filepath.Ext(path)] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
