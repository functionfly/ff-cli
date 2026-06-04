package flypy

import (
	"crypto/sha256"
	"encoding/hex"
)

// moduleCacheKey is the SHA-256 hex digest of a WASM payload, used as
// a key into the module cache. Exposed for tests; not part of the
// public API.
func moduleCacheKey(wasmBytes []byte) string {
	sum := sha256.Sum256(wasmBytes)
	return hex.EncodeToString(sum[:])
}
