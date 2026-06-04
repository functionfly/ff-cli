package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher manages file system watching for hot reload functionality
type FileWatcher struct {
	watcher      *fsnotify.Watcher
	watchedFiles []string
	onChange     func(string)
}

// NewFileWatcher creates a new file watcher
func NewFileWatcher(onChange func(string)) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &FileWatcher{
		watcher:      w,
		watchedFiles: []string{},
		onChange:     onChange,
	}, nil
}

// WatchFiles adds files to the watch list
func (fw *FileWatcher) WatchFiles(files []string) error {
	for _, file := range files {
		// Check if file exists
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue // Skip non-existent files
		}

		// Add file to watcher
		if err := fw.watcher.Add(file); err != nil {
			// Log but don't fail - some files might not be watchable
			fmt.Printf("Warning: failed to watch file %s: %v\n", file, err)
			continue
		}

		fw.watchedFiles = append(fw.watchedFiles, file)
	}
	return nil
}

// Start begins watching for file changes
func (fw *FileWatcher) Start() {
	go func() {
		for {
			select {
			case event, ok := <-fw.watcher.Events:
				if !ok {
					return
				}

				// Only trigger on write events (file modifications)
				if event.Has(fsnotify.Write) {
					fw.onChange(event.Name)
				}

			case err, ok := <-fw.watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("File watcher error: %v\n", err)
			}
		}
	}()
}

// Stop stops watching files
func (fw *FileWatcher) Stop() error {
	return fw.watcher.Close()
}

// GetWatchedFiles returns the list of currently watched files
func (fw *FileWatcher) GetWatchedFiles() []string {
	return fw.watchedFiles
}

// IdentifyWatchableFiles identifies which files should be watched for a given runtime
func IdentifyWatchableFiles(runtime string) []string {
	files := []string{}

	// Always watch manifest files
	manifestFiles := []string{"functionfly.jsonc", "functionfly.json"}
	for _, mf := range manifestFiles {
		if _, err := os.Stat(mf); err == nil {
			files = append(files, mf)
		}
	}

	// Watch entry files based on runtime
	var entryFiles []string
	switch runtime {
	case "node18", "node20", "deno":
		entryFiles = []string{"index.js", "main.js", "index.ts", "main.ts"}
	case "python3.11":
		entryFiles = []string{"main.py", "index.py"}
	default:
		// Fallback
		entryFiles = []string{"index.js", "main.js", "index.ts", "main.ts", "main.py"}
	}

	for _, ef := range entryFiles {
		if _, err := os.Stat(ef); err == nil {
			files = append(files, ef)
		}
	}

	// Watch package/dependency files
	packageFiles := []string{"package.json", "requirements.txt", "pyproject.toml", "Pipfile"}
	for _, pf := range packageFiles {
		if _, err := os.Stat(pf); err == nil {
			files = append(files, pf)
		}
	}

	// For JavaScript/TypeScript, also watch node_modules if it exists
	// (though this might be too aggressive, we'll watch package.json changes instead)
	if runtime == "node18" || runtime == "node20" || runtime == "deno" {
		if _, err := os.Stat("package.json"); err == nil {
			// Watch package-lock.json, yarn.lock, etc.
			lockFiles := []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml"}
			for _, lf := range lockFiles {
				if _, err := os.Stat(lf); err == nil {
					files = append(files, lf)
				}
			}
		}
	}

	return files
}

// ShouldIgnoreFile checks if a file should be ignored during watching
func ShouldIgnoreFile(filename string) bool {
	// Ignore temporary files, build artifacts, and common editor files
	ignorePatterns := []string{
		"*.tmp",
		"*.temp",
		"*.swp",
		"*.bak",
		"*.log",
		"node_modules/",
		".git/",
		"dist/",
		"build/",
		"target/",
		"*.wasm",
		"functionfly-local",
		"functionfly",
		"fly",
		"ff",
		"migrate",
		"health-monitor",
		"orchestrator-api",
	}

	for _, pattern := range ignorePatterns {
		if strings.Contains(filename, pattern) {
			return true
		}
		// Check for file extension matches
		if strings.HasSuffix(filename, strings.TrimPrefix(pattern, "*")) {
			return true
		}
	}

	return false
}

// WatchDirectory recursively watches a directory for changes
func (fw *FileWatcher) WatchDirectory(dir string, excludePatterns []string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}

		// Skip if matches exclude patterns
		for _, pattern := range excludePatterns {
			if strings.Contains(path, pattern) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Only watch files, not directories
		if !info.IsDir() {
			return fw.watcher.Add(path)
		}

		return nil
	})
}
