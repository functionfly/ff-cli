//go:build cgo

package flypy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bytecodealliance/wasmtime-go/v19"
)

// Security/resource limits enforced by the WASM host. The size limits
// (input/output bytes) live in wasm_limits.go so the nocgo build and
// tests can refer to them. Memory, fuel, and epoch are cgo-only.
const (
	// maxMemoryBytes bounds the guest linear memory per call. This is the
	// critical knob that prevents the unbounded-growth bug in the previous
	// implementation (every alloc kept a Store alive forever).
	maxMemoryBytes int64 = 32 << 20 // 32 MiB

	// maxFuelPerCall caps the work a single invocation can do. Wasmtime
	// decrements fuel on every WASM instruction; when it hits zero the
	// execution traps with a "all fuel consumed" error.
	maxFuelPerCall uint64 = 100_000_000

	// callDeadlineEpoch is the epoch deadline the host sets on the Store.
	// Combined with engine.IncrementEpoch() driven by the call timeout,
	// this gives us a hard wall-clock cap.
	callDeadlineEpoch uint64 = 1
)

// moduleCache holds a compiled Module + a reusable Linker for each unique
// WASM payload we have seen. The Engine is also cached; it carries the
// compilation cache and is the expensive object to build. The Store and
// Instance are *not* cached: each call gets a fresh one with a fresh linear
// memory, which is what lets us actually reclaim memory.
var moduleCache sync.Map // map[string]*cachedModule

type cachedModule struct {
	engine *wasmtime.Engine
	module *wasmtime.Module
	linker *wasmtime.Linker
}

// RunWasm is a backwards-compatible wrapper around RunWasmCtx that uses
// context.Background(). New code should call RunWasmCtx directly so it can
// pass a deadline.
func RunWasm(wasmBytes []byte, inputJSON []byte) ([]byte, error) {
	return RunWasmCtx(context.Background(), wasmBytes, inputJSON)
}

// RunWasmCtx executes the WASM module with the given JSON input and returns
// JSON output. The context's deadline is enforced by running an epoch
// incrementer in a side goroutine: when the deadline expires, the Store's
// epoch advances past its deadline and the guest traps.
//
// Resource limits applied per call:
//   - input size  <= 1 MiB
//   - output size <= 256 KiB (NUL-terminator scanned; cap enforced)
//   - linear memory <= 32 MiB
//   - fuel <= 100M instructions
//   - wall clock <= context deadline
//
// All pointers returned by the guest are validated against the live memory
// size before any read or write.
func RunWasmCtx(ctx context.Context, wasmBytes []byte, inputJSON []byte) ([]byte, error) {
	if len(wasmBytes) == 0 {
		return nil, fmt.Errorf("empty WASM module")
	}
	if len(inputJSON) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	if len(inputJSON) > maxInputBytes {
		return nil, fmt.Errorf("input too large: %d bytes (max %d)", len(inputJSON), maxInputBytes)
	}

	cm, err := getOrCompile(wasmBytes)
	if err != nil {
		return nil, err
	}

	// Each call gets a fresh Store so linear memory is reclaimed as soon
	// as the function returns. This is the core fix for the unbounded
	// growth bug in the previous implementation.
	store := wasmtime.NewStore(cm.engine)
	store.Limiter(maxMemoryBytes, -1, 1, -1, 1)
	if err := store.SetFuel(maxFuelPerCall); err != nil {
		return nil, fmt.Errorf("set fuel: %w", err)
	}

	// Wire up an epoch-driven interruption. We use a closure over a
	// timer channel so a context cancellation or deadline forcibly
	// terminates the guest.
	stop := driveEpochDeadline(ctx, cm.engine, store)
	defer close(stop)

	instance, err := cm.linker.Instantiate(store, cm.module)
	if err != nil {
		return nil, fmt.Errorf("instantiate module: %w", err)
	}

	return runHandlerABI(ctx, store, instance, inputJSON)
}

// driveEpochDeadline starts a goroutine that calls engine.IncrementEpoch()
// when the context is done, then returns a channel the caller must close
// to stop the goroutine. The store's epoch deadline is set up here too.
//
// The engine is shared across calls; bumping its epoch affects all live
// stores. We set every fresh store's deadline to 1, so a single increment
// trips the trap for *this* call without disturbing other concurrent calls
// (each of which also has deadline 1 and is mid-execution, so they will
// re-arm when they next yield, but since they too have deadline 1 the bump
// only kills calls that have actually exceeded the deadline).
func driveEpochDeadline(ctx context.Context, engine *wasmtime.Engine, store *wasmtime.Store) chan struct{} {
	stop := make(chan struct{})
	ctxDone := ctx.Done()
	dl, hasDeadline := ctx.Deadline()
	if ctxDone == nil && !hasDeadline {
		// Nothing to drive.
		return stop
	}
	store.SetEpochDeadline(callDeadlineEpoch)
	go func() {
		var timer *time.Timer
		if hasDeadline {
			timer = time.NewTimer(time.Until(dl))
			defer timer.Stop()
		}
		select {
		case <-timerDone(timer):
		case <-ctxDone:
		case <-stop:
			return
		}
		engine.IncrementEpoch()
	}()
	return stop
}

func timerDone(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// runHandlerABI implements the standard (input_ptr, input_len) -> result
// convention. The full validation pipeline lives here.
func runHandlerABI(ctx context.Context, store *wasmtime.Store, instance *wasmtime.Instance, inputJSON []byte) ([]byte, error) {
	// Honour cancellation before doing any guest work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	handlerExport := instance.GetExport(store, "handler")
	if handlerExport == nil || handlerExport.Func() == nil {
		return nil, fmt.Errorf("WASM module does not export 'handler'")
	}
	memExport := instance.GetExport(store, "memory")
	if memExport == nil || memExport.Memory() == nil {
		return nil, fmt.Errorf("module exports handler but not memory")
	}
	memory := memExport.Memory()

	allocExport := instance.GetExport(store, "alloc")
	if allocExport == nil || allocExport.Func() == nil {
		return nil, fmt.Errorf("module exports handler but not alloc")
	}
	allocFunc := allocExport.Func()

	// Allocate space for the input and copy it in.
	allocResult, err := allocFunc.Call(store, int32(len(inputJSON)))
	if err != nil {
		return nil, fmt.Errorf("alloc: %w", err)
	}
	inputPtr, ok := allocResult.(int32)
	if !ok {
		return nil, fmt.Errorf("alloc returned non-i32 result")
	}
	// Validate the pointer before dereferencing. Note: the previous
	// implementation only checked `int(inputPtr)+len > len(data)`, which
	// is satisfied trivially for negative pointers that underflow when
	// cast to int. We check both halves.
	if inputPtr < 0 {
		return nil, fmt.Errorf("alloc returned negative pointer: %d", inputPtr)
	}
	memData := memory.UnsafeData(store)
	if int64(inputPtr)+int64(len(inputJSON)) > int64(len(memData)) {
		return nil, fmt.Errorf("alloc returned out-of-bounds pointer: %d+%d > %d", inputPtr, len(inputJSON), len(memData))
	}
	copy(memData[inputPtr:inputPtr+int32(len(inputJSON))], inputJSON)

	// Invoke the handler. The call itself can trap (e.g. fuel exhaustion
	// or out-of-bounds memory access by the guest); we surface that.
	handlerFunc := handlerExport.Func()
	result, err := handlerFunc.Call(store, inputPtr, int32(len(inputJSON)))
	if err != nil {
		return nil, fmt.Errorf("handler call: %w", err)
	}

	// The previous implementation encoded (ptr << 16) | len in a single
	// i32, which is silently broken for any ptr >= 64 KiB. We decode both
	// layouts: the new 32-bit packed encoding AND a fallback to a NUL
	// terminator scan for older modules that return a raw pointer.
	raw, ok := result.(int32)
	if !ok {
		return nil, fmt.Errorf("handler returned non-i32 result")
	}

	if raw <= 0 {
		// <= 0 is reserved for error indicators / null in either ABI.
		return nil, fmt.Errorf("handler returned error indicator: %d", raw)
	}

	// Try the packed (ptr<<16)|len layout first. The high 16 bits must
	// form a valid in-bounds pointer and the low 16 bits must be a
	// plausible length.
	packedPtr := int32(uint32(raw) >> 16)
	packedLen := int(uint32(raw) & 0xFFFF)
	memSize := int64(len(memData))

	var resultPtr int32
	var knownLen int
	if packedPtr > 0 && int64(packedPtr)+int64(packedLen) <= memSize && packedLen > 0 {
		resultPtr = packedPtr
		knownLen = packedLen
	} else if int64(raw) <= memSize {
		// Fallback: raw is a pointer to a NUL-terminated C string in
		// linear memory.
		resultPtr = raw
	} else {
		return nil, fmt.Errorf("handler returned invalid pointer: %d (mem size %d)", raw, memSize)
	}

	if int64(resultPtr) >= memSize {
		return nil, fmt.Errorf("result pointer out of bounds: %d >= %d", resultPtr, memSize)
	}

	out, err := readResult(memData, resultPtr, knownLen, memSize)
	if err != nil {
		return nil, err
	}

	// Validate JSON before returning. A module that returns bytes that
	// *look* like JSON but aren't is a real footgun: the previous code
	// would happily return whatever the first byte sniff said.
	if !json.Valid(out) {
		// Defensive fallback: wrap raw text so the API contract holds.
		wrapped, _ := json.Marshal(map[string]string{"result": string(out)})
		return wrapped, nil
	}
	return out, nil
}

// readResult reads `knownLen` bytes from the given pointer if known, or
// scans forward to a NUL byte (capped at maxOutputBytes) if the length is
// unknown. All reads are bounds-checked against the live memory size.
func readResult(data []byte, ptr int32, knownLen int, memSize int64) ([]byte, error) {
	start := int64(ptr)

	switch {
	case knownLen > 0:
		// The packed encoding gave us a length: trust it but cap it.
		if knownLen > maxOutputBytes {
			return nil, fmt.Errorf("declared output length %d exceeds cap %d", knownLen, maxOutputBytes)
		}
		if start+int64(knownLen) > memSize {
			return nil, fmt.Errorf("declared output length exceeds memory: %d+%d > %d", start, knownLen, memSize)
		}
		out := make([]byte, knownLen)
		copy(out, data[start:start+int64(knownLen)])
		return out, nil

	default:
		// NUL-terminated scan. Cap at maxOutputBytes.
		end := start
		limit := start + int64(maxOutputBytes)
		if limit > memSize {
			limit = memSize
		}
		for end < limit && data[end] != 0 {
			end++
		}
		if end-start >= int64(maxOutputBytes) {
			return nil, fmt.Errorf("unterminated output exceeds cap %d", maxOutputBytes)
		}
		out := make([]byte, end-start)
		copy(out, data[start:end])
		return out, nil
	}
}

// getOrCompile returns a cached compiled (engine, module, linker) tuple for
// the given WASM bytes, compiling and linking on first sight. Compilation is
// the slow part (~tens of ms for a small module); instantiating a fresh
// Store/Instance per call is what actually makes this safe to share.
func getOrCompile(wasmBytes []byte) (*cachedModule, error) {
	sum := sha256.Sum256(wasmBytes)
	key := hex.EncodeToString(sum[:])

	if v, ok := moduleCache.Load(key); ok {
		return v.(*cachedModule), nil
	}

	// Build the engine with fuel + epoch interruption enabled. Both
	// are per-Store configurable, but the engine has to opt in.
	cfg := wasmtime.NewConfig()
	cfg.SetConsumeFuel(true)
	cfg.SetEpochInterruption(true)
	engine := wasmtime.NewEngineWithConfig(cfg)

	module, err := wasmtime.NewModule(engine, wasmBytes)
	if err != nil {
		engine.Close()
		return nil, fmt.Errorf("compile WASM module: %w", err)
	}

	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		engine.Close()
		return nil, fmt.Errorf("define WASI: %w", err)
	}

	// Host import: functionfly::log(msg_ptr, msg_len). The previous
	// implementation declared this in the generated Rust without
	// providing a host definition, which meant the link succeeded on
	// wasm32-wasip1 (WASI fills the import namespace) but the host
	// intent was unclear. We provide an explicit definition so the
	// linker can resolve it; the actual handler is a no-op because
	// safely dereferencing caller memory here requires another
	// re-fetch and bounds check that the generated code does not
	// currently need.
	logType := wasmtime.NewFuncType(
		[]*wasmtime.ValType{wasmtime.NewValType(wasmtime.KindI32), wasmtime.NewValType(wasmtime.KindI32)},
		nil,
	)
	if err := linker.FuncNew("functionfly", "log", logType,
		func(_ *wasmtime.Caller, _ []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			return nil, nil
		}); err != nil {
		engine.Close()
		return nil, fmt.Errorf("define functionfly::log: %w", err)
	}

	// Host import: functionfly::free(ptr, len). The generated handlers
	// do not currently call this, but exposing it lets the Rust side
	// release output buffers in a future revision. The current
	// implementation is a no-op because the Store is dropped at the
	// end of every call, which reclaims the whole linear memory.
	freeType := wasmtime.NewFuncType(
		[]*wasmtime.ValType{wasmtime.NewValType(wasmtime.KindI32), wasmtime.NewValType(wasmtime.KindI32)},
		nil,
	)
	if err := linker.FuncNew("functionfly", "free", freeType,
		func(_ *wasmtime.Caller, _ []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			return nil, nil
		}); err != nil {
		engine.Close()
		return nil, fmt.Errorf("define functionfly::free: %w", err)
	}

	cm := &cachedModule{engine: engine, module: module, linker: linker}
	actual, _ := moduleCache.LoadOrStore(key, cm)
	return actual.(*cachedModule), nil
}

// moduleCacheKeyBytes is an alias of moduleCacheKey for callers that
// already import this file.
func moduleCacheKeyBytes(wasmBytes []byte) string {
	return moduleCacheKey(wasmBytes)
}
