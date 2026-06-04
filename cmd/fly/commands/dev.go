package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	fmt.Printf("🚀 FunctionFly local runtime\n")
	fmt.Printf("   Function: %s v%s\n", manifest.Name, manifest.Version)
	fmt.Printf("   Runtime:  %s\n", manifest.Runtime)
	fmt.Printf("   URL:      http://localhost:%d\n", port)
	if watch {
		fmt.Printf("   Watching: enabled\n")
	}
	fmt.Printf("\nPress Ctrl+C to stop\n\n")
	funcFile, err := findFunctionFile(manifest)
	if err != nil {
		return err
	}
	handler := newLocalHandler(manifest, funcFile)
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.ServeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "function": manifest.Name, "version": manifest.Version})
	})
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}
	if watch {
		go watchFiles(funcFile, handler)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()
	<-ctx.Done()
	fmt.Printf("\n🛑 Shutting down...\n")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

type localHandler struct {
	manifest *Manifest
	funcFile string
	reloaded chan struct{}
}

func newLocalHandler(manifest *Manifest, funcFile string) *localHandler {
	return &localHandler{manifest: manifest, funcFile: funcFile, reloaded: make(chan struct{}, 1)}
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-FunctionFly-Function", h.manifest.Name)
	w.Header().Set("X-FunctionFly-Version", h.manifest.Version)
	w.Header().Set("X-FunctionFly-Runtime", "local")
	if len(body) == 0 {
		body = []byte(`"hello"`)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		logrus.WithError(err).Error("Failed to write response")
		return
	}
	latency := time.Since(start).Milliseconds()
	fmt.Printf("[%s] %s %s → 200 (%dms)\n", time.Now().Format("15:04:05"), r.Method, r.URL.Path, latency)
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
