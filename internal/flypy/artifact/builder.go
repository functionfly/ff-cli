package artifact

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// BuildInput contains the input for building an artifact
type BuildInput struct {
	WasmModule    []byte
	IRModule      *ir.Module
	Name          string
	Version       string
	SignKey       []byte
	Deterministic bool
}

// Artifact represents a compiled FlyPy artifact bundle
type Artifact struct {
	WasmModule      []byte
	Manifest        *Manifest
	CapabilityMap   *CapabilityMap
	DeterminismHash string
	Signature       []byte
}

// Manifest contains metadata about the function
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

// CapabilityMap declares the capabilities required by the function
type CapabilityMap struct {
	FunctionID   string                 `json:"function_id"`
	Requested    []string               `json:"requested"`
	Approved     []string               `json:"approved"`
	Denied       []string               `json:"denied"`
	Restrictions map[string]interface{} `json:"restrictions,omitempty"`
}

// Build creates an artifact bundle from the input.
// Returns an error if the IR module contains no functions.
func Build(input BuildInput) (*Artifact, error) {
	// Guard: require at least one function in the IR module
	if input.IRModule == nil || len(input.IRModule.Functions) == 0 {
		return nil, fmt.Errorf("IR module contains no functions: a 'handler' function is required")
	}

	// Generate manifest
	manifest := generateManifest(input)

	// Generate capability map
	capMap := generateCapabilityMap(input)

	// Compute determinism hash
	detHash := computeDeterminismHash(input.IRModule)

	// Sign the artifact
	var signature []byte
	if len(input.SignKey) > 0 {
		sig, err := signArtifact(input.SignKey, detHash)
		if err != nil {
			return nil, fmt.Errorf("failed to sign artifact: %w", err)
		}
		signature = sig
	}

	return &Artifact{
		WasmModule:      input.WasmModule,
		Manifest:        manifest,
		CapabilityMap:   capMap,
		DeterminismHash: detHash,
		Signature:       signature,
	}, nil
}

func generateManifest(input BuildInput) *Manifest {
	// Extract parameter types from IR
	inputSchema := make(map[string]interface{})
	for _, param := range input.IRModule.Functions[0].Parameters {
		inputSchema[param.Name] = map[string]string{
			"type": param.Type.String(),
		}
	}

	return &Manifest{
		FlypyVersion:  "1.0.0",
		Name:          input.Name,
		Version:       input.Version,
		Runtime:       "flypy-deterministic",
		InputSchema:   inputSchema,
		OutputSchema:  map[string]interface{}{},
		Deterministic: input.Deterministic,
		Idempotent:    true,
		SideEffects:   "none",
		Capabilities:  []string{},
		CompiledAt:    time.Now().UTC().Format(time.RFC3339),
		PythonVersion: "3.12",
	}
}

func generateCapabilityMap(input BuildInput) *CapabilityMap {
	// For deterministic mode, no capabilities needed
	return &CapabilityMap{
		FunctionID: generateFunctionID(input.Name),
		Requested:  []string{},
		Approved:   []string{},
		Denied:     []string{},
	}
}

func computeDeterminismHash(module *ir.Module) string {
	// Create a canonical representation of the IR
	canonical := fmt.Sprintf("flypy-v1|%s", module.Name)

	for _, fn := range module.Functions {
		canonical += fmt.Sprintf("|%s", fn.Name)
		for _, param := range fn.Parameters {
			canonical += fmt.Sprintf("|%s:%s", param.Name, param.Type)
		}
		for _, op := range fn.Body {
			canonical += fmt.Sprintf("|%s:%s", op.Type, op.Result)
		}
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:])
}

func signArtifact(privateKey []byte, data string) ([]byte, error) {
	// Parse the private key
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size")
	}

	// Sign the data
	signature := ed25519.Sign(ed25519.PrivateKey(privateKey), []byte(data))
	return signature, nil
}

func generateFunctionID(name string) string {
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:8])
}

// VerifySignature verifies the artifact signature
func VerifySignature(artifact *Artifact, publicKey []byte) error {
	if len(artifact.Signature) == 0 {
		return fmt.Errorf("no signature present")
	}

	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size")
	}

	valid := ed25519.Verify(ed25519.PublicKey(publicKey), []byte(artifact.DeterminismHash), artifact.Signature)
	if !valid {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// ToJSON serializes the manifest to JSON
func (m *Manifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ToJSON serializes the capability map to JSON
func (c *CapabilityMap) ToJSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// GenerateSigningKey generates a new Ed25519 signing key
func GenerateSigningKey() ([]byte, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return publicKey, privateKey, nil
}
