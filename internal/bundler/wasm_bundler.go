package bundler

import (
	"github.com/functionfly/ff-cli/internal/manifest"
)

// BundleForWasmRuntime bundles code for WebAssembly runtime execution
// Implements actual WebAssembly compilation using QuickJS (via Javy) for JavaScript
// and Pyodide/micropython for Python, with fallback to WAT templates
func BundleForWasmRuntime(manifest *manifest.Manifest) ([]byte, error) {
	return BundleForWasmRuntimeWithWorkingDirectory(manifest, "")
}

// BundleForWasmRuntimeWithWorkingDirectory bundles code for WebAssembly runtime execution
// with explicit working directory support for consistent path resolution.
// Dependencies are automatically installed if specified in the manifest.
func BundleForWasmRuntimeWithWorkingDirectory(manifest *manifest.Manifest, workingDir string) ([]byte, error) {
	if manifest == nil {
		return nil, NewBundlerError("wasm bundle", "manifest cannot be nil")
	}

	// Resolve and validate working directory
	resolvedDir, err := ResolveWorkingDirectory(workingDir)
	if err != nil {
		return nil, NewBundlerErrorWithCause("wasm bundle", "failed to resolve working directory", err)
	}

	// Execute bundling within the specified working directory
	var result []byte
	err = WithWorkingDirectory(resolvedDir, func() error {
		// Install dependencies first
		if depErr := InstallDependencies(manifest); depErr != nil {
			return NewBundlerErrorWithCause("wasm bundle", "dependency installation failed", depErr)
		}

		var bundleErr error
		switch manifest.Runtime {
		case "node18", "node20", "deno", "bun", "wasm":
			result, bundleErr = bundleJSForWasmRuntime(manifest)
		case "python3.11", "python3.12", "python":
			result, bundleErr = bundlePythonForWasmRuntime(manifest)
		case "go", "c", "kotlin", "ruby", "swift", "microvm", "prism":
			// Native runtimes — package source for server-side compilation
			result, bundleErr = bundleNative(manifest)
		default:
			result, bundleErr = bundleJSForWasmRuntime(manifest) // Default to JS
		}
		return bundleErr
	})

	return result, err
}
