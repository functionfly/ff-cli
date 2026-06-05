package flypy

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/functionfly/ff-cli/internal/telemetry"
)

// LocalRuntimeConfig holds configuration for the local runtime.
//
// The zero value is usable but not safe to expose to the network: callers
// running anything other than a single-developer loopback test should set
// AuthToken, raise MaxConcurrentExecutions deliberately, and pin the bind
// address explicitly.
type LocalRuntimeConfig struct {
	ArtifactPath string
	Host         string
	Port         int
	Verbose      bool

	// MaxRequestBytes bounds the size of an HTTP request body. 0 means
	// use the default of 1 MiB. Applies via http.MaxBytesReader.
	MaxRequestBytes int64

	// MaxConcurrentExecutions caps how many handler invocations can run
	// at once. Additional requests block until a slot frees up. 0 means
	// use the default of 8.
	MaxConcurrentExecutions int

	// PerIPRateLimit is the per-client request rate in requests/second.
	// 0 means use the default of 100.
	PerIPRateLimit rate.Limit

	// PerIPRateBurst is the per-client burst allowance. 0 means use the
	// default of 200.
	PerIPRateBurst int

	// ExecutionTimeout caps a single handler invocation. 0 means use
	// the default of 30 s.
	ExecutionTimeout time.Duration

	// AuthToken, if non-empty, requires every request to carry
	// `Authorization: Bearer <AuthToken>`. Health and info endpoints
	// are also gated so a leaked URL doesn't leak the manifest.
	AuthToken string

	// VerifySignature, if true, requires signature.sig to verify against
	// SignaturePublicKey. The default (false) is appropriate for local
	// development; production should set this.
	VerifySignature      bool
	SignaturePublicKey   []byte
}

// LocalRuntime provides a local execution environment for FlyPy functions.
type LocalRuntime struct {
	config *LocalRuntimeConfig

	// artifact is swapped atomically by Reload. Reads from request
	// handlers use Load() so concurrent Reload calls don't race the
	// in-flight requests.
	artifact atomic.Pointer[Artifact]

	server *http.Server

	// sem bounds concurrent handler executions. Buffered to
	// MaxConcurrentExecutions; each handler sends on entry and receives
	// on exit.
	sem chan struct{}

	// limiters is a per-IP token bucket map. Entries are evicted by
	// the cleanup goroutine below.
	limiters   map[string]*ipLimiter
	limitersMu sync.Mutex

	// stopCleanup is closed by Stop to terminate the limiter GC goroutine.
	// stopOnce guards close() so Stop() is idempotent and safe to call
	// from both Reload() and the process shutdown path.
	stopCleanup chan struct{}
	stopOnce    sync.Once
}

type ipLimiter struct {
	limiter *rate.Limiter
	last    atomic.Int64 // unix nanos; updated on every Allow
}

// defaults applied in NewLocalRuntime when the zero value is used.
const (
	defaultMaxRequestBytes         int64         = 1 << 20 // 1 MiB
	defaultMaxConcurrentExecutions               = 8
	defaultPerIPRateLimit          rate.Limit   = 100
	defaultPerIPRateBurst                       = 200
	defaultExecutionTimeout        time.Duration = 30 * time.Second
	limiterIdleTTL                               = 10 * time.Minute
)

// NewLocalRuntime creates a new local runtime instance.
func NewLocalRuntime(config *LocalRuntimeConfig) (*LocalRuntime, error) {
	if config == nil {
		config = &LocalRuntimeConfig{}
	}
	applyDefaults(config)

	// Load the artifact up front so misconfiguration is caught before
	// the server starts listening.
	artifact, err := loadArtifactFromPath(config.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load artifact: %w", err)
	}
	if config.VerifySignature {
		if err := verifyArtifactSignature(artifact, config.SignaturePublicKey); err != nil {
			return nil, fmt.Errorf("artifact signature verification failed: %w", err)
		}
	}

	r := &LocalRuntime{
		config:      config,
		sem:         make(chan struct{}, config.MaxConcurrentExecutions),
		limiters:    make(map[string]*ipLimiter),
		stopCleanup: make(chan struct{}),
	}
	r.artifact.Store(artifact)

	return r, nil
}

func applyDefaults(c *LocalRuntimeConfig) {
	if c.MaxRequestBytes <= 0 {
		c.MaxRequestBytes = defaultMaxRequestBytes
	}
	if c.MaxConcurrentExecutions <= 0 {
		c.MaxConcurrentExecutions = defaultMaxConcurrentExecutions
	}
	if c.PerIPRateLimit == 0 {
		c.PerIPRateLimit = defaultPerIPRateLimit
	}
	if c.PerIPRateBurst == 0 {
		c.PerIPRateBurst = defaultPerIPRateBurst
	}
	if c.ExecutionTimeout <= 0 {
		c.ExecutionTimeout = defaultExecutionTimeout
	}
	// Default the bind address to loopback. Binding to all interfaces
	// must be an explicit opt-in.
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
}

// Start starts the local runtime server.
func (r *LocalRuntime) Start(ctx context.Context) error {
	// Re-arm the stop channel for the case where Reload() invokes
	// Start() after a previous Stop().
	r.stopOnce = sync.Once{}
	r.stopCleanup = make(chan struct{})
	go r.evictIdleLimiters()

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", r.withMiddleware(r.handleHealth))

	// Function info endpoint
	mux.HandleFunc("/info", r.withMiddleware(r.handleInfo))

	// Function execution endpoint
	mux.HandleFunc("/", r.withMiddleware(r.handleExecute))

	r.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", r.config.Host, r.config.Port),
		Handler: mux,

		// Production-safe timeouts to prevent slow-client attacks.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	if r.config.Verbose && r.config.Host != "127.0.0.1" && r.config.Host != "localhost" {
		logrus.WithField("addr", r.server.Addr).Warn("binding to non-loopback address; ensure the network is trusted")
	}

	// Start server in a goroutine
	go func() {
		if r.config.Verbose {
			fmt.Printf("   Starting server on %s:%d\n", r.config.Host, r.config.Port)
		}
		if err := r.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the local runtime server and background goroutines.
// Safe to call multiple times; only the first call has any effect.
func (r *LocalRuntime) Stop(ctx context.Context) error {
	r.stopOnce.Do(func() { close(r.stopCleanup) })
	if r.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return r.server.Shutdown(shutdownCtx)
	}
	return nil
}

// Reload reloads the artifact and restarts the server.
func (r *LocalRuntime) Reload(ctx context.Context) error {
	if err := r.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	// Reload the artifact off the hot path of in-flight requests by
	// building it before publishing the new pointer.
	artifact, err := loadArtifactFromPath(r.config.ArtifactPath)
	if err != nil {
		return fmt.Errorf("failed to reload artifact: %w", err)
	}
	if r.config.VerifySignature {
		if err := verifyArtifactSignature(artifact, r.config.SignaturePublicKey); err != nil {
			return fmt.Errorf("artifact signature verification failed: %w", err)
		}
	}
	r.artifact.Store(artifact)

	// Restart the server. Start spins up a new background listener and
	// returns once it's accepting (ListenAndServe is fire-and-forget,
	// but the next Reload is what blocks if the listener failed to
	// bind).
	return r.Start(ctx)
}

// requestIDKey is the context key for the per-request ID.
type requestIDKey struct{}

// withMiddleware applies the cross-cutting concerns: max body, auth,
// per-IP rate limit, and structured access logging. Handlers can assume all
// of these checks have passed.
func (r *LocalRuntime) withMiddleware(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Request-ID", newRequestID())

		if r.config.AuthToken != "" {
			tok := bearerToken(req)
			if tok != r.config.AuthToken {
			r.logRequest(req, w, http.StatusUnauthorized)
			logrus.WithField("remote", req.RemoteAddr).Warn("auth rejected: invalid bearer token")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		if !r.allow(req) {
			r.logRequest(req, w, http.StatusTooManyRequests)
			logrus.WithField("remote", req.RemoteAddr).Warn("request rejected: rate limit exceeded")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Body limit is enforced through MaxBytesReader; further
		// downstream the JSON decoder will see a clean error.
		req.Body = http.MaxBytesReader(w, req.Body, r.config.MaxRequestBytes)
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey{}, w.Header().Get("X-Request-ID")))

		h(w, req)
	}
}

func (r *LocalRuntime) logRequest(req *http.Request, resp http.ResponseWriter, status int) {
	logrus.WithFields(logrus.Fields{
		"request_id": resp.Header().Get("X-Request-ID"),
		"method":     req.Method,
		"path":       req.URL.Path,
		"remote":     req.RemoteAddr,
		"status":     status,
	}).Info("handled request")
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}

func bearerToken(req *http.Request) string {
	h := req.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[len("Bearer "):])
}

// allow performs a per-IP rate-limit check, lazily creating a limiter
// for previously-unseen clients.
func (r *LocalRuntime) allow(req *http.Request) bool {
	ip := clientIP(req)
	now := time.Now().UnixNano()

	r.limitersMu.Lock()
	l, ok := r.limiters[ip]
	if !ok {
		l = &ipLimiter{limiter: rate.NewLimiter(r.config.PerIPRateLimit, r.config.PerIPRateBurst)}
		r.limiters[ip] = l
	}
	r.limitersMu.Unlock()

	l.last.Store(now)
	return l.limiter.Allow()
}

// clientIP extracts the client's IP, preferring trusted forwarded
// headers only if explicitly configured. By default we honour r.RemoteAddr
// directly to avoid header-injection attacks.
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// evictIdleLimiters runs until stopCleanup is closed, removing per-IP
// limiter entries that haven't been touched in limiterIdleTTL. This
// prevents the map from growing unboundedly under attack.
func (r *LocalRuntime) evictIdleLimiters() {
	ticker := time.NewTicker(limiterIdleTTL / 2)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCleanup:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-limiterIdleTTL).UnixNano()
			r.limitersMu.Lock()
			for ip, l := range r.limiters {
				if l.last.Load() < cutoff {
					delete(r.limiters, ip)
				}
			}
			r.limitersMu.Unlock()
		}
	}
}

// handleHealth handles health check requests.
func (r *LocalRuntime) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a := r.artifact.Load()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"function":  a.Manifest.Name,
		"version":   a.Manifest.Version,
		"runtime":   "flypy-local",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleInfo handles function information requests.
func (r *LocalRuntime) handleInfo(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a := r.artifact.Load()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"name":             a.Manifest.Name,
		"version":          a.Manifest.Version,
		"deterministic":    a.Manifest.Deterministic,
		"capabilities":     a.CapabilityMap.Requested,
		"input_schema":     a.Manifest.InputSchema,
		"output_schema":    a.Manifest.OutputSchema,
		"determinism_hash": a.DeterminismHash,
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleExecute handles function execution requests.
func (r *LocalRuntime) handleExecute(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Acquire a concurrency slot before doing anything expensive.
	// We don't time this out: a slot that never frees would mean the
	// runtime is broken in a way a deadline can't fix.
	select {
	case r.sem <- struct{}{}:
	case <-req.Context().Done():
		http.Error(w, "request cancelled", http.StatusServiceUnavailable)
		return
	}
	defer func() { <-r.sem }()

	// Parse request body. MaxBytesReader will turn a too-large body
	// into an error here.
	var input map[string]interface{}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		// A body that's too large surfaces as *http.MaxBytesError
		// since Go 1.19; map that to 413.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			r.logRequest(req, w, http.StatusRequestEntityTooLarge)
			logrus.WithError(err).Warn("rejected: request body too large")
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.logRequest(req, w, http.StatusBadRequest)
		logrus.WithError(err).Warn("rejected: invalid JSON input")
		http.Error(w, "Invalid JSON input", http.StatusBadRequest)
		return
	}

	if r.config.Verbose {
		rid, _ := req.Context().Value(requestIDKey{}).(string)
		logrus.WithFields(logrus.Fields{"request_id": rid, "input": input}).Info("executing handler")
	}

	// Per-call timeout. The context is detached from the request
	// context so a slow client cancelling its request doesn't leave
	// the guest running with the slot still held.
	callCtx, cancel := context.WithTimeout(context.Background(), r.config.ExecutionTimeout)
	defer cancel()

	telemetry.Emit(req.Context(), telemetry.Event{Kind: telemetry.EventLocalExecStart, Start: time.Now()})
	output, err := r.executeWasm(callCtx, input)
	if err != nil {
		logrus.WithError(err).Error("WASM execution failed")
		telemetry.Emit(req.Context(), telemetry.Event{Kind: telemetry.EventLocalExecEnd, Status: "error", Error: err.Error(), Start: time.Now()})
		http.Error(w, "function execution failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		logrus.WithError(err).Error("failed to encode response")
		telemetry.Emit(req.Context(), telemetry.Event{Kind: telemetry.EventLocalExecEnd, Status: "error", Error: err.Error(), Start: time.Now()})
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	telemetry.Emit(req.Context(), telemetry.Event{Kind: telemetry.EventLocalExecEnd, Status: "ok", Start: time.Now()})
	r.logRequest(req, w, http.StatusOK)
}

func (r *LocalRuntime) executeWasm(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	a := r.artifact.Load()
	if a == nil || a.WasmModule == nil {
		return nil, fmt.Errorf("no WASM artifact available")
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	outputJSON, err := RunWasmCtx(ctx, a.WasmModule, inputJSON)
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
	output["hash"] = a.DeterminismHash
	return output, nil
}

// loadArtifactFromPath loads a FlyPy artifact from the specified directory.
//
// The directory is canonicalised and required to be a real directory. A
// missing path or a path containing a `..` segment that escapes the
// current working directory is rejected.
func loadArtifactFromPath(artifactPath string) (*Artifact, error) {
	if artifactPath == "" {
		return nil, fmt.Errorf("artifact path is empty")
	}
	cleaned, err := validateArtifactPath(artifactPath)
	if err != nil {
		return nil, err
	}

	// Load manifest
	manifestPath := filepath.Join(cleaned, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Load Wasm module
	wasmPath := filepath.Join(cleaned, "state_transition.wasm")
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Wasm module: %w", err)
	}

	// Load capability map
	capPath := filepath.Join(cleaned, "capability.map")
	capData, err := os.ReadFile(capPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read capability map: %w", err)
	}

	var capMap CapabilityMap
	if err := json.Unmarshal(capData, &capMap); err != nil {
		return nil, fmt.Errorf("failed to parse capability map: %w", err)
	}

	// Load determinism hash
	hashPath := filepath.Join(cleaned, "determinism.hash")
	hashData, err := os.ReadFile(hashPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read determinism hash: %w", err)
	}

	// Load signature (optional)
	sigPath := filepath.Join(cleaned, "signature.sig")
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

// validateArtifactPath canonicalises the path, confirms it is a real
// directory, and rejects `..` segments that escape the working dir.
func validateArtifactPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("artifact path is empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	cleaned := filepath.Clean(abs)
	info, err := os.Stat(cleaned)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("artifact directory not found: %s", cleaned)
		}
		return "", fmt.Errorf("stat artifact path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("artifact path is not a directory: %s", cleaned)
	}
	// Lstat the components to refuse symlinks. The artifact directory
	// must be a real dir, not a symlink to /etc.
	if li, err := os.Lstat(cleaned); err == nil && li.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("artifact path is a symlink: %s", cleaned)
	}
	return cleaned, nil
}

// verifyArtifactSignature validates the artifact's Ed25519 signature.
func verifyArtifactSignature(a *Artifact, publicKey []byte) error {
	if a == nil {
		return fmt.Errorf("nil artifact")
	}
	if len(a.Signature) == 0 {
		return fmt.Errorf("artifact has no signature")
	}
	if len(publicKey) == 0 {
		return fmt.Errorf("no public key configured")
	}
	return VerifyArtifactSignature(a, publicKey)
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
