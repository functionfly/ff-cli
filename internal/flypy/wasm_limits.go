package flypy

// Resource limits enforced by the WASM host. These are defined in a
// non-cgo file so the test suite and the nocgo build can refer to
// them. The actual host logic (engine, fuel, epoch, limiter) lives
// in wasm_exec_cgo.go; on the nocgo build the limits still apply
// conceptually, the runtime just returns "WASM not available" before
// they matter.

const (
	// MaxInputBytes is the maximum size of the JSON input that the
	// host will copy into the guest's linear memory. Anything larger
	// is rejected before any allocation happens.
	MaxInputBytes = 1 << 20 // 1 MiB

	// MaxOutputBytes is the maximum number of bytes the host will
	// read back from the guest after a successful handler invocation.
	// This caps both memory pressure and JSON-encoder time.
	MaxOutputBytes = 1 << 18 // 256 KiB
)

// Compatibility aliases for the cgo build, which references these by
// the unexported names. Keeping two names here avoids touching the
// cgo file again.
const (
	maxInputBytes  = MaxInputBytes
	maxOutputBytes = MaxOutputBytes
)
