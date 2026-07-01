package bundler

import (
	"github.com/functionfly/ff-cli/internal/manifest"
)

// Bundle creates a bundled version of the function code.
// It handles different runtime types and delegates to appropriate bundlers.
// Dependencies are automatically installed if specified in the manifest.
func Bundle(manifest *manifest.Manifest) ([]byte, error) {
	return BundleWithOptions(manifest, nil)
}

// BundleWithOptions creates a bundled version of the function code with custom options.
// It handles different runtime types and delegates to appropriate bundlers.
// Dependencies are automatically installed if specified in the manifest.
func BundleWithOptions(manifest *manifest.Manifest, options *BundleOptions) ([]byte, error) {
	if manifest == nil {
		return nil, NewBundlerError("bundle", "manifest cannot be nil")
	}

	// Install dependencies first
	if err := InstallDependencies(manifest); err != nil {
		return nil, NewBundlerErrorWithCause("bundle", "dependency installation failed", err)
	}

	switch manifest.Runtime {
	case "node18", "node20", "deno", "bun":
		// If WASM target is specified, try TypeScript-to-WASM compilation first
		if options != nil && options.TargetWASM {
			if wasmBytes, err := bundleTypeScriptWASM(manifest); err == nil {
				return wasmBytes, nil
			}
		}
		return bundleJavaScript(manifest, options)
	case "python3.11":
		return bundlePython(manifest)
	case "typescript-wasm":
		return bundleTypeScriptWASM(manifest)
	case "go", "c", "kotlin", "ruby", "swift", "microvm":
		// These runtimes compile natively on the platform — no client-side
		// bundling needed. The source is uploaded as-is and compiled server-side.
		return bundleNative(manifest)
	case "wasm":
		// Alias for browser-wasm — compile to WASM
		return bundleWasmAlias(manifest)
	case "prism":
		// Prism is a polyglot managed runtime — upload source as-is
		return bundleNative(manifest)
	default:
		return nil, &RuntimeNotSupportedError{
			Runtime:   manifest.Runtime,
			Supported: []string{"node18", "node20", "deno", "bun", "python3.11", "typescript-wasm", "wasm", "prism", "go", "c", "kotlin", "ruby", "swift", "microvm"},
		}
	}
}

// BundleWithWorkingDirectory creates a bundled version of the function code
// with explicit working directory support for consistent path resolution.
// Dependencies are automatically installed if specified in the manifest.
func BundleWithWorkingDirectory(manifest *manifest.Manifest, workingDir string) ([]byte, error) {
	return BundleWithOptionsAndWorkingDirectory(manifest, nil, workingDir)
}

// BundleWithOptionsAndWorkingDirectory creates a bundled version of the function code
// with explicit working directory support and custom options.
// Dependencies are automatically installed if specified in the manifest.
func BundleWithOptionsAndWorkingDirectory(manifest *manifest.Manifest, options *BundleOptions, workingDir string) ([]byte, error) {
	if manifest == nil {
		return nil, NewBundlerError("bundle", "manifest cannot be nil")
	}

	// Resolve and validate working directory
	resolvedDir, err := ResolveWorkingDirectory(workingDir)
	if err != nil {
		return nil, NewBundlerErrorWithCause("bundle", "failed to resolve working directory", err)
	}

	// Execute bundling within the specified working directory
	var result []byte
	err = WithWorkingDirectory(resolvedDir, func() error {
		var bundleErr error
		result, bundleErr = BundleWithOptions(manifest, options)
		return bundleErr
	})

	return result, err
}

// bundleWasmAlias handles the "wasm" runtime, which is an alias for "browser-wasm".
func bundleWasmAlias(m *manifest.Manifest) ([]byte, error) {
	return BundleForWasmRuntime(m)
}
