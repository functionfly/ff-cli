//go:build !cgo

package flypy

import (
	"context"
	"fmt"
)

// RunWasm executes the WASM module. When cgo is disabled, WASM execution is
// unavailable and this returns an error so the caller can fall back to mock mode.
func RunWasm(wasmBytes []byte, inputJSON []byte) ([]byte, error) {
	return nil, fmt.Errorf("WASM execution requires cgo (install wasmtime); build with CGO_ENABLED=1")
}

// RunWasmCtx is the context-aware variant; the nocgo build always returns
// an error so the caller can fall back to mock mode.
func RunWasmCtx(_ context.Context, _ []byte, _ []byte) ([]byte, error) {
	return nil, fmt.Errorf("WASM execution requires cgo (install wasmtime); build with CGO_ENABLED=1")
}
