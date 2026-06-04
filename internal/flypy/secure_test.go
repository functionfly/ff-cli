package flypy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// (cgo-only RunWasmCtx tests live in secure_test_cgo.go.)

// TestValidateArtifactPath covers the directory validator that
// protects against symlink and `..` traversal.
func TestValidateArtifactPath(t *testing.T) {
	tmp := t.TempDir()

	if _, err := validateArtifactPath(tmp); err != nil {
		t.Fatalf("valid dir rejected: %v", err)
	}

	missing := filepath.Join(tmp, "does-not-exist")
	if _, err := validateArtifactPath(missing); err == nil {
		t.Fatal("missing dir accepted")
	}

	// A file is not a directory.
	file := filepath.Join(tmp, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := validateArtifactPath(file); err == nil {
		t.Fatal("file accepted as dir")
	}
}

// TestValidateArtifactPathEmpty ensures the empty-string case is
// rejected before filepath.Abs gets a chance to return ".".
func TestValidateArtifactPathEmpty(t *testing.T) {
	if _, err := validateArtifactPath(""); err == nil {
		t.Fatal("empty path accepted")
	}
}

// TestApplyDefaults asserts the security-relevant defaults land
// when callers pass a zero-value config.
func TestApplyDefaults(t *testing.T) {
	c := &LocalRuntimeConfig{}
	applyDefaults(c)
	if c.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %q", c.Host)
	}
	if c.MaxRequestBytes != defaultMaxRequestBytes {
		t.Errorf("default MaxRequestBytes not applied")
	}
	if c.MaxConcurrentExecutions != defaultMaxConcurrentExecutions {
		t.Errorf("default MaxConcurrentExecutions not applied")
	}
	if c.ExecutionTimeout != defaultExecutionTimeout {
		t.Errorf("default ExecutionTimeout not applied")
	}
}

// TestRateLimitMiddleware confirms an IP gets rate-limited after the
// burst allowance is spent.
func TestRateLimitMiddleware(t *testing.T) {
	c := &LocalRuntimeConfig{
		PerIPRateLimit: 1,
		PerIPRateBurst: 1,
	}
	applyDefaults(c)
	r := &LocalRuntime{
		config:      c,
		limiters:    map[string]*ipLimiter{},
		stopCleanup: make(chan struct{}),
	}
	defer close(r.stopCleanup)

	// First request is allowed (the burst bucket has one token).
	if !r.allow(httptest.NewRequest(http.MethodPost, "/", nil)) {
		t.Fatal("first request should be allowed")
	}
	// Second request from the same IP is rejected.
	if r.allow(httptest.NewRequest(http.MethodPost, "/", nil)) {
		t.Fatal("second request should be rate-limited")
	}
}

// TestAuthMiddleware asserts the bearer-token check is enforced when
// configured.
func TestAuthMiddleware(t *testing.T) {
	c := &LocalRuntimeConfig{AuthToken: "secret"}
	applyDefaults(c)
	r := &LocalRuntime{
		config:      c,
		limiters:    map[string]*ipLimiter{},
		stopCleanup: make(chan struct{}),
	}
	defer close(r.stopCleanup)

	handler := r.withMiddleware(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No token -> 401
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	// Wrong token -> 401
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	handler(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rr.Code)
	}

	// Correct token -> 200
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", rr.Code)
	}
}

// TestStopIdempotent guards against the double-close panic that would
// happen if Reload() and the process shutdown both call Stop.
func TestStopIdempotent(t *testing.T) {
	r := &LocalRuntime{
		config:      &LocalRuntimeConfig{},
		stopCleanup: make(chan struct{}),
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// Second call must not panic on close(stopCleanup).
	if err := r.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestVerifyArtifactSignatureWithoutSignature ensures a missing
// signature is rejected when verification is on.
func TestVerifyArtifactSignatureWithoutSignature(t *testing.T) {
	a := &Artifact{DeterminismHash: "abc", Signature: nil}
	pub := make([]byte, 32)
	err := VerifyArtifactSignature(a, pub)
	if err == nil {
		t.Fatal("expected error for missing signature, got nil")
	}
}

// TestVerifyArtifactSignatureBadKey covers the public-key size check.
func TestVerifyArtifactSignatureBadKey(t *testing.T) {
	a := &Artifact{DeterminismHash: "abc", Signature: []byte("anything")}
	err := VerifyArtifactSignature(a, []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key, got nil")
	}
}

// TestModuleCacheKeyDeterministic guards a small but important
// property: the cache key is a function of the bytes only.
func TestModuleCacheKeyDeterministic(t *testing.T) {
	a := moduleCacheKey([]byte("hello"))
	b := moduleCacheKey([]byte("hello"))
	if a != b {
		t.Fatalf("cache key not deterministic: %q vs %q", a, b)
	}
	if a == moduleCacheKey([]byte("world")) {
		t.Fatalf("different inputs produced the same key")
	}
}

// Compile-time anchor so the json and time imports aren't dropped.
var (
	_ = json.Valid
	_ = time.Second
)
