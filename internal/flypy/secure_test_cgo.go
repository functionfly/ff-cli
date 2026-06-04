//go:build cgo

package flypy

import (
	"context"
	"strings"
	"testing"
)

// TestRunWasmCtxRejectsEmptyInput verifies that the host refuses to
// dispatch a call without input rather than passing a zero-length
// payload to the guest allocator.
func TestRunWasmCtxRejectsEmptyInput(t *testing.T) {
	_, err := RunWasmCtx(context.Background(), []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, nil)
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
	if !strings.Contains(err.Error(), "empty input") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunWasmCtxRejectsOversizeInput verifies the 1 MiB cap is enforced
// before any guest work happens.
func TestRunWasmCtxRejectsOversizeInput(t *testing.T) {
	huge := make([]byte, MaxInputBytes+1)
	_, err := RunWasmCtx(context.Background(), []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, huge)
	if err == nil {
		t.Fatal("expected error for oversize input, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunWasmCtxRejectsEmptyModule guards against an obvious
// misuse that the previous version also caught.
func TestRunWasmCtxRejectsEmptyModule(t *testing.T) {
	_, err := RunWasmCtx(context.Background(), nil, []byte(`{"a":1}`))
	if err == nil {
		t.Fatal("expected error for empty WASM, got nil")
	}
}

// TestRunWasmCtxRejectsNonWasm checks the magic number is validated
// by the underlying engine.
func TestRunWasmCtxRejectsNonWasm(t *testing.T) {
	_, err := RunWasmCtx(context.Background(), []byte("not wasm at all"), []byte(`{"a":1}`))
	if err == nil {
		t.Fatal("expected error for non-WASM, got nil")
	}
}
