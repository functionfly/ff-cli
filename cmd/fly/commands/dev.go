package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func NewDevCmd() *cobra.Command {
	var port int
	var watch bool
	var noWatch bool
	cmd := &cobra.Command{
		Use:     "dev",
		Short:   "Run your function locally",
		Example: "  ff dev\n  ff dev --port 8080\n  ff dev --watch",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := LoadConfig()
			if port == 0 {
				port = cfg.Dev.Port
				if port == 0 {
					port = 8787
				}
			}
			enableWatch := watch || (cfg.Dev.Watch && !noWatch)
			return runDev(port, enableWatch)
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 0, "Port to listen on (default: 8787)")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for file changes and auto-reload")
	cmd.Flags().BoolVar(&noWatch, "no-watch", false, "Disable file watching")
	return cmd
}

func runDev(port int, watch bool) error {
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	funcFile, err := findFunctionFile(manifest)
	if err != nil {
		return err
	}
	handler := newLocalHandler(manifest, funcFile)
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.ServeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "function": manifest.Name, "version": manifest.Version})
	})
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("could not bind to port %d: %w\n   → Is another process using this port? Try --port <other>", port, err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	fmt.Printf("🚀 FunctionFly local runtime\n")
	fmt.Printf("   Function: %s v%s\n", manifest.Name, manifest.Version)
	fmt.Printf("   Runtime:  %s\n", manifest.Runtime)
	fmt.Printf("   URL:      http://localhost:%d\n", actualPort)
	if watch {
		fmt.Printf("   Watching: enabled\n")
	}
	fmt.Printf("\nPress Ctrl+C to stop\n\n")

	if watch {
		go watchFiles(funcFile, handler)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}
	fmt.Printf("\n🛑 Shutting down...\n")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

type localHandler struct {
	manifest *Manifest
	funcFile string
	executor *pythonExecutor
	reloaded chan struct{}
}

func newLocalHandler(manifest *Manifest, funcFile string) *localHandler {
	return &localHandler{
		manifest: manifest,
		funcFile: funcFile,
		executor: newPythonExecutor(manifest, funcFile),
		reloaded: make(chan struct{}, 1),
	}
}

func (h *localHandler) reload() {
	fmt.Printf("♻️  Reloading %s...\n", h.funcFile)
	select {
	case h.reloaded <- struct{}{}:
	default:
	}
}

func (h *localHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("X-FunctionFly-Function", h.manifest.Name)
	w.Header().Set("X-FunctionFly-Version", h.manifest.Version)
	w.Header().Set("X-FunctionFly-Runtime", "local-python")

	// Invoke the real handler. The executor subprocess-insulates us from
	// user-code crashes; any error surfaces as a 500 with the message in
	// the body so curl/ff-test can see it.
	result, execErr := h.executor.Execute(r, body)
	if execErr != nil {
		// Some handlers (e.g. the hello-world template) don't expose a
		// runtime that the dev server can run (JS/TS). Surface a clear
		// message instead of a generic 500.
		if strings.Contains(execErr.Error(), "dev runtime only supports Python") {
			http.Error(w, execErr.Error(), http.StatusNotImplemented)
		} else if strings.Contains(execErr.Error(), "no Python 3 interpreter") {
			http.Error(w, execErr.Error(), http.StatusNotImplemented)
		} else {
			http.Error(w, execErr.Error(), http.StatusInternalServerError)
		}
		logrus.WithError(execErr).Error("handler execution failed")
		latency := time.Since(start).Milliseconds()
		fmt.Printf("[%s] %s %s → 500 (%dms): %v\n", time.Now().Format("15:04:05"), r.Method, r.URL.Path, latency, execErr)
		return
	}

	// Copy headers from the handler's response, but never let the handler
	// override our routing headers.
	w.Header().Del("Content-Type") // always enforce application/json; drop any user-set value
	for k, vs := range result.Headers {
		if k == "Content-Type" {
			continue // enforce JSON below
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(result.Status)
	if len(result.Body) > 0 {
		_, _ = w.Write(result.Body)
	}
	latency := time.Since(start).Milliseconds()
	fmt.Printf("[%s] %s %s → %d (%dms)\n", time.Now().Format("15:04:05"), r.Method, r.URL.Path, result.Status, latency)
}

func watchFiles(funcFile string, handler *localHandler) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not start file watcher: %v\n", err)
		return
	}
	defer watcher.Close()
	dir := filepath.Dir(funcFile)
	if err := watcher.Add(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not watch %s: %v\n", dir, err)
		return
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				handler.reload()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "Watcher error: %v\n", err)
		}
	}
}

func findFunctionFile(manifest *Manifest) (string, error) {
	candidates := []string{"index.js", "index.ts", "main.py", "handler.js", "handler.ts", "handler.py"}
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			return f, nil
		}
	}
	return "", fmt.Errorf("no function file found\n   → Expected one of: %v", candidates)
}
