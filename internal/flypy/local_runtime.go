package flypy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// LocalRuntimeConfig holds configuration for the local runtime
type LocalRuntimeConfig struct {
	ArtifactPath string
	Host         string
	Port         int
	Verbose      bool
}

// LocalRuntime provides a local execution environment for FlyPy functions
type LocalRuntime struct {
	config   *LocalRuntimeConfig
	server   *http.Server
	artifact *Artifact
}

// NewLocalRuntime creates a new local runtime instance
func NewLocalRuntime(config *LocalRuntimeConfig) (*LocalRuntime, error) {
	// Load the artifact
	artifact, err := loadArtifactFromPath(config.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load artifact: %w", err)
	}

	return &LocalRuntime{
		config:   config,
		artifact: artifact,
	}, nil
}

// Start starts the local runtime server
func (r *LocalRuntime) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", r.handleHealth)

	// Function info endpoint
	mux.HandleFunc("/info", r.handleInfo)

	// Function execution endpoint
	mux.HandleFunc("/", r.handleExecute)

	r.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", r.config.Host, r.config.Port),
		Handler: mux,

		// Production-safe timeouts to prevent slow-client attacks
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Start server in a goroutine
	go func() {
		if r.config.Verbose {
			fmt.Printf("   Starting server on %s:%d\n", r.config.Host, r.config.Port)
		}
		if err := r.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the local runtime server
func (r *LocalRuntime) Stop(ctx context.Context) error {
	if r.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		return r.server.Shutdown(shutdownCtx)
	}
	return nil
}

// Reload reloads the artifact and restarts the server
func (r *LocalRuntime) Reload(ctx context.Context) error {
	// Stop the current server
	if err := r.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	// Reload the artifact
	artifact, err := loadArtifactFromPath(r.config.ArtifactPath)
	if err != nil {
		return fmt.Errorf("failed to reload artifact: %w", err)
	}

	// Update the artifact
	r.artifact = artifact

	// Restart the server
	return r.Start(ctx)
}

// handleHealth handles health check requests
func (r *LocalRuntime) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"function":  r.artifact.Manifest.Name,
		"version":   r.artifact.Manifest.Version,
		"runtime":   "flypy-local",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleInfo handles function information requests
func (r *LocalRuntime) handleInfo(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"name":             r.artifact.Manifest.Name,
		"version":          r.artifact.Manifest.Version,
		"deterministic":    r.artifact.Manifest.Deterministic,
		"capabilities":     r.artifact.CapabilityMap.Requested,
		"input_schema":     r.artifact.Manifest.InputSchema,
		"output_schema":    r.artifact.Manifest.OutputSchema,
		"determinism_hash": r.artifact.DeterminismHash,
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleExecute handles function execution requests
func (r *LocalRuntime) handleExecute(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var input map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON input", http.StatusBadRequest)
		return
	}

	if r.config.Verbose {
		fmt.Printf("   Executing function with input: %+v\n", input)
	}

	// Execute the function using WASM runtime if available
	// Otherwise fall back to mock response for development
	output, err := r.executeWasm(input)
	if err != nil {
		// Fall back to mock response if WASM execution fails
		logrus.WithError(err).Warn("WASM execution failed, using mock response")
		output = map[string]interface{}{
			"result":    "function executed (mock mode)",
			"input":     input,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"mode":      "mock",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(output); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// executeWasm executes the function using WASM runtime (wasmtime when built with cgo)
func (r *LocalRuntime) executeWasm(input map[string]interface{}) (map[string]interface{}, error) {
	if r.artifact == nil || r.artifact.WasmModule == nil {
		return nil, fmt.Errorf("no WASM artifact available")
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	outputJSON, err := RunWasm(r.artifact.WasmModule, inputJSON)
	if err != nil {
		return nil, err
	}

	var output map[string]interface{}
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		return nil, fmt.Errorf("parse WASM output: %w", err)
	}

	// Ensure common fields for compatibility
	if output == nil {
		output = make(map[string]interface{})
	}
	output["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	output["mode"] = "wasm"
	output["hash"] = r.artifact.DeterminismHash
	return output, nil
}

// loadArtifactFromPath loads a FlyPy artifact from the specified directory
func loadArtifactFromPath(artifactPath string) (*Artifact, error) {
	// Load manifest
	manifestPath := filepath.Join(artifactPath, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Load Wasm module
	wasmPath := filepath.Join(artifactPath, "state_transition.wasm")
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Wasm module: %w", err)
	}

	// Load capability map
	capPath := filepath.Join(artifactPath, "capability.map")
	capData, err := os.ReadFile(capPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read capability map: %w", err)
	}

	var capMap CapabilityMap
	if err := json.Unmarshal(capData, &capMap); err != nil {
		return nil, fmt.Errorf("failed to parse capability map: %w", err)
	}

	// Load determinism hash
	hashPath := filepath.Join(artifactPath, "determinism.hash")
	hashData, err := os.ReadFile(hashPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read determinism hash: %w", err)
	}

	// Load signature (optional)
	sigPath := filepath.Join(artifactPath, "signature.sig")
	var signature []byte
	if sigData, err := os.ReadFile(sigPath); err == nil {
		signature = sigData
	}

	return &Artifact{
		Manifest:        &manifest,
		WasmModule:      wasmData,
		CapabilityMap:   &capMap,
		DeterminismHash: string(hashData),
		Signature:       signature,
	}, nil
}

// Artifact represents a compiled FlyPy artifact (local copy for local runtime)
type Artifact struct {
	Manifest        *Manifest
	WasmModule      []byte
	CapabilityMap   *CapabilityMap
	DeterminismHash string
	Signature       []byte
}

// Manifest contains metadata about the function (local copy)
type Manifest struct {
	FlypyVersion  string                 `json:"flypy_version"`
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Runtime       string                 `json:"runtime"`
	InputSchema   map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema  map[string]interface{} `json:"output_schema,omitempty"`
	Deterministic bool                   `json:"deterministic"`
	Idempotent    bool                   `json:"idempotent"`
	SideEffects   string                 `json:"side_effects"`
	Capabilities  []string               `json:"capabilities"`
	CompiledAt    string                 `json:"compiled_at"`
	PythonVersion string                 `json:"python_version"`
}

// CapabilityMap declares the capabilities required by the function (local copy)
type CapabilityMap struct {
	FunctionID   string                 `json:"function_id"`
	Requested    []string               `json:"requested"`
	Approved     []string               `json:"approved"`
	Denied       []string               `json:"denied"`
	Restrictions map[string]interface{} `json:"restrictions,omitempty"`
}
