package flypy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/functionfly/ff-cli/internal/flypy/artifact"
	"github.com/functionfly/ff-cli/internal/flypy/backend"
	"github.com/functionfly/ff-cli/internal/flypy/compiler"
	"github.com/functionfly/ff-cli/internal/flypy/ir"
	"github.com/functionfly/ff-cli/internal/flypy/parser"
	"github.com/functionfly/ff-cli/internal/flypy/restrictions"
	"github.com/functionfly/ff-cli/internal/flypy/verifier"
)

// Config holds FlyPy configuration options
type Config struct {
	// Mode determines the execution mode
	Mode ExecutionMode

	// OutputDir is where compiled artifacts are written
	OutputDir string

	// Verbose enables detailed logging
	Verbose bool

	// SignKey is the Ed25519 private key for signing artifacts
	SignKey []byte

	// TargetWasm specifies the Wasm target (default: wasm32-unknown-unknown)
	TargetWasm string

	// Version specifies the function version (default: 1.0.0)
	Version string

	// CompileTimeout is the maximum time allowed for the full compilation pipeline.
	// Defaults to 5 minutes if zero.
	CompileTimeout time.Duration

	// Logger is the structured logger to use. Defaults to logrus.StandardLogger().
	Logger *logrus.Logger

	// AutoFallback enables automatic fallback to CompatibleMode when the requested
	// mode fails due to unsupported Python features (classes, generators, decorators).
	// When true, a warning is added to the result instead of returning an error.
	AutoFallback bool
}

// ExecutionMode defines how the function will be executed
type ExecutionMode string

const (
	// DeterministicMode compiles to pure Wasm with full determinism
	// Only allows: json, math, typing, collections
	DeterministicMode ExecutionMode = "deterministic"

	// ComplexMode allows extended stdlib modules while maintaining determinism
	// Allows: csv, io (StringIO/BytesIO), re, datetime, itertools, functools, etc.
	ComplexMode ExecutionMode = "complex"

	// CompatibleMode allows some non-deterministic operations (with warnings)
	// Uses MicroPython fallback for full Python compatibility
	CompatibleMode ExecutionMode = "compatible"

	// defaultCompileTimeout is used when Config.CompileTimeout is zero.
	defaultCompileTimeout = 5 * time.Minute
)

// Result contains the result of a FlyPy compilation
type Result struct {
	// Artifact is the compiled and signed artifact bundle
	Artifact *artifact.Artifact

	// Warnings contains any warnings encountered during compilation
	Warnings []string

	// DeterminismProof contains proof of determinism for verification
	DeterminismProof *DeterminismProof

	// SideEffects contains the side effect analysis results
	SideEffects []verifier.SideEffect

	// SideEffectSummary provides a summary of side effects by type
	SideEffectSummary map[verifier.SideEffectType]int
}

// DeterminismProof contains cryptographic proof of determinism
type DeterminismProof struct {
	// IRHash is the SHA-256 hash of the canonical IR
	IRHash string

	// Timestamp is when the proof was generated
	Timestamp string

	// Capabilities are the declared capabilities
	Capabilities []string
}

// Compiler is the main FlyPy compiler
type Compiler struct {
	config *Config
	log    *logrus.Logger
}

// NewCompiler creates a new FlyPy compiler with the given configuration
func NewCompiler(config *Config) *Compiler {
	if config.OutputDir == "" {
		config.OutputDir = "./dist"
	}
	if config.TargetWasm == "" {
		config.TargetWasm = "wasm32-unknown-unknown"
	}
	if config.CompileTimeout == 0 {
		config.CompileTimeout = defaultCompileTimeout
	}

	log := config.Logger
	if log == nil {
		log = logrus.StandardLogger()
	}

	return &Compiler{
		config: config,
		log:    log,
	}
}

// NewCompilerWithDefaults creates a new FlyPy compiler with default configuration
func NewCompilerWithDefaults() *Compiler {
	return NewCompiler(&Config{
		Mode:      DeterministicMode,
		OutputDir: "./dist",
		Verbose:   false,
	})
}

// Compile compiles Python source code to a deterministic Wasm artifact.
// It enforces the CompileTimeout from Config to prevent runaway compilations.
// If AutoFallback is enabled and the requested mode fails due to unsupported features,
// it automatically retries with CompatibleMode (MicroPython runtime).
func (c *Compiler) Compile(ctx context.Context, source string, name string) (*Result, error) {
	compileCtx, cancel := context.WithTimeout(ctx, c.config.CompileTimeout)
	defer cancel()

	result, err := c.compile(compileCtx, source, name)
	if err != nil && c.config.AutoFallback && c.config.Mode != CompatibleMode {
		errStr := err.Error()
		isUnsupported := strings.Contains(errStr, "restriction violations") ||
			strings.Contains(errStr, "UNSUPPORTED_FEATURE") ||
			strings.Contains(errStr, "FORBIDDEN_FEATURE") ||
			strings.Contains(errStr, "class") ||
			strings.Contains(errStr, "generator") ||
			strings.Contains(errStr, "decorator")

		if isUnsupported {
			c.log.WithFields(logrus.Fields{
				"function":      name,
				"original_mode": string(c.config.Mode),
				"fallback_mode": string(CompatibleMode),
			}).Warn("Auto-falling back to CompatibleMode due to unsupported features")

			fallbackConfig := *c.config
			fallbackConfig.Mode = CompatibleMode
			fallbackCompiler := NewCompiler(&fallbackConfig)

			fallbackResult, fallbackErr := fallbackCompiler.compile(compileCtx, source, name)
			if fallbackErr != nil {
				return nil, fmt.Errorf("original error: %w; fallback also failed: %v", err, fallbackErr)
			}

			fallbackResult.Warnings = append(fallbackResult.Warnings,
				fmt.Sprintf("Auto-fallback: compiled with CompatibleMode (MicroPython) because %s mode failed: %s. "+
					"CompatibleMode may have non-deterministic behavior and is not recommended for production workloads.",
					string(c.config.Mode), errStr),
			)
			return fallbackResult, nil
		}
	}

	return result, err
}

// compile is the internal implementation of Compile with a pre-configured context.
func (c *Compiler) compile(ctx context.Context, source string, name string) (*Result, error) {
	log := c.log.WithFields(logrus.Fields{
		"function": name,
		"mode":     string(c.config.Mode),
	})

	if c.config.Verbose {
		log.Info("Starting compilation")
	}

	// Phase 1: Parse Python source to AST
	if c.config.Verbose {
		log.Info("Phase 1: Parsing Python AST")
	}
	pythonAST, err := parser.ParsePython(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Python: %w", err)
	}

	// Phase 1.5: Pre-check that a handler function exists
	if c.config.Verbose {
		log.Info("Phase 1.5: Validating handler function")
	}
	if err := validateHandlerExists(pythonAST); err != nil {
		return nil, err
	}

	// Phase 2: Enforce restricted subset (mode-aware)
	if c.config.Verbose {
		log.Info("Phase 2: Enforcing restricted subset")
	}
	restrictionErrors := restrictions.EnforceWithMode(pythonAST, restrictions.ExecutionMode(c.config.Mode))
	if len(restrictionErrors) > 0 {
		return nil, fmt.Errorf("restriction violations: %v", restrictionErrors)
	}

	// Phase 3: Generate IR
	if c.config.Verbose {
		log.Info("Phase 3: Generating deterministic IR")
	}
	irModule, err := ir.Generate(pythonAST, name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate IR: %w", err)
	}

	// Phase 4: Verify determinism
	if c.config.Verbose {
		log.Info("Phase 4: Verifying determinism")
	}
	verificationErrors := verifier.Verify(irModule)
	if len(verificationErrors) > 0 {
		return nil, fmt.Errorf("determinism verification failed: %v", verificationErrors)
	}

	// Phase 4.5: Analyze side effects
	if c.config.Verbose {
		log.Info("Phase 4.5: Analyzing side effects")
	}
	sideEffectAnalyzer := verifier.NewSideEffectAnalyzer(irModule)
	sideEffects := sideEffectAnalyzer.Analyze()

	// Check for side effects that violate determinism
	for _, effect := range sideEffects {
		if effect.Type == verifier.SideEffectNetwork ||
			effect.Type == verifier.SideEffectExternalState ||
			effect.Type == verifier.SideEffectIO {
			return nil, fmt.Errorf("side effect violation: %s in function %s", effect.Message, effect.Function)
		}
	}

	// Phase 5: Generate Rust code (mode-aware)
	if c.config.Verbose {
		log.Info("Phase 5: Generating Rust code")
	}
	rustCode, err := backend.GenerateRustWithMode(irModule, string(c.config.Mode))
	if err != nil {
		return nil, fmt.Errorf("failed to generate Rust: %w", err)
	}

	// Phase 6: Compile to Wasm (with context for cancellation)
	if c.config.Verbose {
		log.Info("Phase 6: Compiling to Wasm")
	}
	wasmBytes, err := compiler.CompileRustWithModeCtxWithLog(ctx, rustCode, c.config.TargetWasm, string(c.config.Mode), c.log)
	if err != nil {
		return nil, fmt.Errorf("failed to compile Wasm: %w", err)
	}

	// Phase 7: Build artifact bundle
	if c.config.Verbose {
		log.Info("Phase 7: Building artifact bundle")
	}
	version := c.config.Version
	if version == "" {
		version = "1.0.0"
	}

	artifactBundle, err := artifact.Build(artifact.BuildInput{
		WasmModule:    wasmBytes,
		IRModule:      irModule,
		Name:          name,
		Version:       version,
		SignKey:       c.config.SignKey,
		Deterministic: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build artifact: %w", err)
	}

	// Write output files
	if err := c.writeOutput(artifactBundle); err != nil {
		return nil, fmt.Errorf("failed to write output: %w", err)
	}

	if c.config.Verbose {
		log.WithField("output_dir", c.config.OutputDir).Info("Build complete")
	}

	return &Result{
		Artifact: artifactBundle,
		Warnings: []string{},
		DeterminismProof: &DeterminismProof{
			IRHash:       artifactBundle.DeterminismHash,
			Capabilities: artifactBundle.CapabilityMap.Requested,
		},
		SideEffects:       sideEffects,
		SideEffectSummary: sideEffectAnalyzer.GetSideEffectSummary(),
	}, nil
}

// validateHandlerExists checks that the parsed AST contains a function named "handler".
// This provides an early, clear error before the full pipeline runs.
func validateHandlerExists(ast *parser.PythonAST) error {
	functions := parser.GetFunctions(ast)
	for _, fn := range functions {
		if parser.GetFunctionName(fn) == "handler" {
			return nil
		}
	}
	return fmt.Errorf("no 'handler' function found: FlyPy requires a top-level function named 'handler(event)'")
}

// writeOutput writes the artifact bundle to the output directory
func (c *Compiler) writeOutput(artifact *artifact.Artifact) error {
	// Create output directory
	if err := os.MkdirAll(c.config.OutputDir, 0750); err != nil {
		return err
	}

	// Write Wasm module
	wasmPath := filepath.Join(c.config.OutputDir, "state_transition.wasm")
	if err := os.WriteFile(wasmPath, artifact.WasmModule, 0600); err != nil {
		return err
	}

	// Write manifest
	manifestPath := filepath.Join(c.config.OutputDir, "manifest.json")
	manifestJSON, err := json.MarshalIndent(artifact.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, manifestJSON, 0600); err != nil {
		return err
	}

	// Write capability map
	capPath := filepath.Join(c.config.OutputDir, "capability.map")
	capJSON, err := json.MarshalIndent(artifact.CapabilityMap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(capPath, capJSON, 0600); err != nil {
		return err
	}

	// Write determinism hash
	hashPath := filepath.Join(c.config.OutputDir, "determinism.hash")
	if err := os.WriteFile(hashPath, []byte(artifact.DeterminismHash), 0600); err != nil {
		return err
	}

	// Write signature
	sigPath := filepath.Join(c.config.OutputDir, "signature.sig")
	if err := os.WriteFile(sigPath, artifact.Signature, 0600); err != nil {
		return err
	}

	return nil
}

// GetVersion returns the current FlyPy version.
// The version is set at build time via ldflags; falls back to "dev".
func GetVersion() string {
	return Version
}

// SupportsLanguage checks if a language is supported for deterministic compilation
func SupportsLanguage(lang string) bool {
	switch strings.ToLower(lang) {
	case "python", "python3", "py":
		return true
	default:
		return false
	}
}
