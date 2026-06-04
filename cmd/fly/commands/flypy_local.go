/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/flypy"
)

// flypyLocalCmd represents the flypy local command
var flypyLocalCmd = &cobra.Command{
	Use:   "local",
	Short: "Run FlyPy function locally for testing",
	Long: `Runs a compiled FlyPy function locally using a lightweight HTTP server.

This allows you to test your function before deploying it to the registry.
The server provides the same execution environment as production.

Examples:
  ff flypy local
  ff flypy local --port=8080 --artifact=./dist
  ff flypy local --watch`,
	Run: flypyLocalRun,
}

// flypyLocalFlags holds flags specific to the local command
var flypyLocalFlags struct {
	port     int
	watch    bool
	artifact string
	host     string
}

func init() {
	flypyCmd.AddCommand(flypyLocalCmd)

	// Local-specific flags
	flypyLocalCmd.Flags().IntVarP(&flypyLocalFlags.port, "port", "p", 8080, "Port to listen on")
	flypyLocalCmd.Flags().BoolVarP(&flypyLocalFlags.watch, "watch", "w", false, "Watch for changes and reload")
	flypyLocalCmd.Flags().StringVarP(&flypyLocalFlags.artifact, "artifact", "a", "./dist", "Path to compiled artifact directory")
	flypyLocalCmd.Flags().StringVar(&flypyLocalFlags.host, "host", "localhost", "Host to bind to")
}

// flypyLocalRun implements the flypy local command
func flypyLocalRun(cmd *cobra.Command, args []string) {
	artifactPath := flypyLocalFlags.artifact

	// Validate artifact directory exists
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: artifact directory '%s' not found\n", artifactPath)
		fmt.Fprintf(os.Stderr, "Run 'ff flypy build' first to compile your function\n")
		os.Exit(1)
	}

	// Check for required artifact files
	requiredFiles := []string{
		"state_transition.wasm",
		"manifest.json",
		"capability.map",
	}

	for _, file := range requiredFiles {
		filePath := filepath.Join(artifactPath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: required artifact file '%s' not found in '%s'\n", file, artifactPath)
			fmt.Fprintf(os.Stderr, "Run 'ff flypy build' to create a complete artifact\n")
			os.Exit(1)
		}
	}

	if flypyFlags.verbose {
		fmt.Printf("🚀 Starting FlyPy local runtime...\n\n")
		fmt.Printf("   Artifact: %s\n", artifactPath)
		fmt.Printf("   Host: %s\n", flypyLocalFlags.host)
		fmt.Printf("   Port: %d\n", flypyLocalFlags.port)

		if flypyLocalFlags.watch {
			fmt.Printf("   Watch: enabled\n")
		}
		fmt.Printf("\n")
	}

	// Create local runtime
	runtime, err := flypy.NewLocalRuntime(&flypy.LocalRuntimeConfig{
		ArtifactPath: artifactPath,
		Host:         flypyLocalFlags.host,
		Port:         flypyLocalFlags.port,
		Verbose:      flypyFlags.verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create local runtime: %v\n", err)
		os.Exit(1)
	}

	// Start the server
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runtime.Start(serverCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start local runtime: %v\n", err)
		os.Exit(1)
	}

	// Wait for server to be ready
	fmt.Printf("   Waiting for server to be ready...")

	readyURL := fmt.Sprintf("http://%s:%d/health", flypyLocalFlags.host, flypyLocalFlags.port)
	if err := waitForServerReady(readyURL, 10*time.Second); err != nil {
		fmt.Printf(" failed\n")
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(" ready\n\n")

	// Print startup information
	fmt.Printf("✅ FlyPy local runtime started!\n")
	fmt.Printf("   URL: http://%s:%d\n", flypyLocalFlags.host, flypyLocalFlags.port)
	fmt.Printf("\n")
	fmt.Printf("Endpoints:\n")
	fmt.Printf("  POST /         # Execute function\n")
	fmt.Printf("  GET  /health   # Health check\n")
	fmt.Printf("  GET  /info     # Function information\n")
	fmt.Printf("\n")
	fmt.Printf("Test your function:\n")
	fmt.Printf("  curl -X POST http://%s:%d \\\n", flypyLocalFlags.host, flypyLocalFlags.port)
	fmt.Printf("    -H 'Content-Type: application/json' \\\n")
	fmt.Printf("    -d '{\"input\": \"test\"}'\n")
	fmt.Printf("\n")

	if flypyLocalFlags.watch {
		fmt.Printf("🔍 Watching for artifact changes...\n")

		// Start file watching for hot reload
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create file watcher: %v\n", err)
			os.Exit(1)
		}
		defer watcher.Close()

		// Watch the artifact directory
		if err := watcher.Add(artifactPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to watch artifact directory: %v\n", err)
			os.Exit(1)
		}

		// Start watching in a goroutine
		go watchForChanges(watcher, runtime, artifactPath, flypyLocalFlags.host, flypyLocalFlags.port, flypyFlags.verbose)

		fmt.Printf("   Hot reload enabled\n\n")
	}

	fmt.Printf("Press Ctrl+C to stop\n")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	fmt.Printf("\n\n👋 Shutting down local runtime...\n")

	// Stop the runtime
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := runtime.Stop(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error during shutdown: %v\n", err)
	}

	fmt.Printf("✅ Shutdown complete\n")
}

// watchForChanges monitors the artifact directory for changes and triggers hot reload
func watchForChanges(watcher *fsnotify.Watcher, runtime *flypy.LocalRuntime, artifactPath, host string, port int, verbose bool) {
	// Track reloads to avoid rapid successive reloads
	var lastReload time.Time
	reloadCooldown := 500 * time.Millisecond

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if this is a relevant file change
			if !isArtifactFileChange(event, artifactPath) {
				continue
			}

			// Debounce rapid changes
			if time.Since(lastReload) < reloadCooldown {
				continue
			}
			lastReload = time.Now()

			if verbose {
				fmt.Printf("\n🔄 Detected change to %s, reloading...\n", filepath.Base(event.Name))
			} else {
				fmt.Printf("\n🔄 Artifact changed, reloading...\n")
			}

			// Perform hot reload
			if err := performHotReload(runtime, artifactPath, host, port, verbose); err != nil {
				fmt.Fprintf(os.Stderr, "Error during hot reload: %v\n", err)
				fmt.Printf("❌ Hot reload failed, continuing with current version\n")
			} else {
				fmt.Printf("✅ Hot reload successful\n")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "File watcher error: %v\n", err)
		}
	}
}

// isArtifactFileChange checks if the file system event is for a relevant artifact file
func isArtifactFileChange(event fsnotify.Event, artifactPath string) bool {
	// Only care about write operations (file modifications)
	if event.Op&fsnotify.Write != fsnotify.Write {
		return false
	}

	relPath, err := filepath.Rel(artifactPath, event.Name)
	if err != nil {
		return false
	}

	// Check if it's one of the required artifact files
	requiredFiles := []string{
		"state_transition.wasm",
		"manifest.json",
		"capability.map",
	}

	for _, file := range requiredFiles {
		if relPath == file {
			return true
		}
	}

	return false
}

// performHotReload reloads the artifact and restarts the runtime
func performHotReload(runtime *flypy.LocalRuntime, artifactPath, host string, port int, verbose bool) error {
	// Use the runtime's Reload method
	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer reloadCancel()

	if err := runtime.Reload(reloadCtx); err != nil {
		return fmt.Errorf("failed to reload runtime: %w", err)
	}

	// Wait for the reloaded server to be ready
	readyURL := fmt.Sprintf("http://%s:%d/health", host, port)
	if err := waitForServerReady(readyURL, 5*time.Second); err != nil {
		return fmt.Errorf("reloaded runtime failed to start: %w", err)
	}

	return nil
}

// waitForServerReady waits for the server to respond to health checks
func waitForServerReady(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("server failed to start within %v", timeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				continue
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}
