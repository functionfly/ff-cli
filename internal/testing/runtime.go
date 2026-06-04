package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/functionfly/ff-cli/internal/flypy"
	"github.com/functionfly/ff-cli/internal/manifest"
)

// RuntimeResult represents the result of a runtime execution.
type RuntimeResult struct {
	Status    int
	Output    string
	Error     string
	LatencyMs int
}

// Runtime defines the interface for function runtimes.
type Runtime interface {
	Initialize(ctx context.Context, bundle []byte, manifest *manifest.Manifest) error
	Execute(ctx context.Context, input string) (*RuntimeResult, error)
	Cleanup() error
}

// RuntimeRegistry holds available runtimes.
var runtimeRegistry = make(map[string]Runtime)

// RegisterRuntime registers a runtime implementation.
func RegisterRuntime(name string, runtime Runtime) {
	runtimeRegistry[name] = runtime
}

// GetRuntime returns the runtime for the given name, falling back to WASMRuntime.
func GetRuntime(name string) Runtime {
	if r, ok := runtimeRegistry[name]; ok {
		return r
	}
	return &WASMRuntime{}
}

// WASMRuntime executes compiled WASM bundles via wasmtime.
// It supports both the handler(ptr, len) ABI produced by the bundler and
// the _start (WASI command) ABI.
type WASMRuntime struct {
	bundle   []byte
	manifest *manifest.Manifest
}

func (r *WASMRuntime) Initialize(_ context.Context, bundle []byte, m *manifest.Manifest) error {
	if len(bundle) == 0 {
		return fmt.Errorf("empty WASM bundle for function %q", m.Name)
	}
	r.bundle = bundle
	r.manifest = m
	return nil
}

func (r *WASMRuntime) Execute(ctx context.Context, input string) (*RuntimeResult, error) {
	start := time.Now()

	// Normalise: if the caller passes a raw string wrap it in {"input":"..."} so
	// the WASM handler always receives valid JSON.
	inputJSON := []byte(input)
	if len(inputJSON) == 0 || inputJSON[0] != '{' && inputJSON[0] != '[' {
		b, err := json.Marshal(map[string]string{"input": input})
		if err != nil {
			return nil, fmt.Errorf("marshal input: %w", err)
		}
		inputJSON = b
	}

	// Honour context cancellation before hitting wasmtime.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	out, err := flypy.RunWasm(r.bundle, inputJSON)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return &RuntimeResult{
			Status:    500,
			Error:     err.Error(),
			LatencyMs: latencyMs,
		}, nil
	}

	return &RuntimeResult{
		Status:    200,
		Output:    string(out),
		LatencyMs: latencyMs,
	}, nil
}

func (r *WASMRuntime) Cleanup() error { return nil }

func init() {
	wasm := &WASMRuntime{}
	RegisterRuntime("node", wasm)
	RegisterRuntime("python", wasm)
	RegisterRuntime("go", wasm)
	RegisterRuntime("generic", wasm)
}
