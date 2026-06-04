package bundler

import (
	"fmt"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// createFallbackWasmWrapper creates a fallback WASM module when compilation fails
func createFallbackWasmWrapper(sourceCode string, manifest *manifest.Manifest, runtime string) ([]byte, error) {
	fmt.Printf("Warning: WebAssembly compilation failed for %s, using fallback WAT template\n", runtime)

	switch runtime {
	case "python":
		return createPythonWasmTemplateFromSource(sourceCode, manifest)
	case "javascript", "node18", "node20", "deno":
		return createJSWasmWrapperFromSource(sourceCode, manifest)
	default:
		return createJSWasmWrapperFromSource(sourceCode, manifest)
	}
}

// CreateWasmFallback is the public entry point for WASM fallback creation
// used by the bundler when primary compilation fails
func CreateWasmFallback(sourceCode string, manifest *manifest.Manifest, runtime string) ([]byte, error) {
	return createFallbackWasmWrapper(sourceCode, manifest, runtime)
}

// createPythonWasmTemplateFromSource creates WAT template from source code
func createPythonWasmTemplateFromSource(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	return createPythonWasmModule(sourceCode, manifest)
}

// createJSWasmWrapperFromSource creates JS wrapper from source code
func createJSWasmWrapperFromSource(sourceCode string, manifest *manifest.Manifest) ([]byte, error) {
	// Create a JavaScript wrapper that can be executed in a WASM-compatible environment
	wrapper := fmt.Sprintf(`
// FunctionFly WebAssembly Fallback Wrapper for %s
const sourceCode = %q;

// WASM-compatible execution environment
globalThis.console = {
  log: (...args) => {
    // Output will be captured by WASM host
    const message = args.map(String).join(' ');
    // In real WASM, this would write to memory
  },
  error: (...args) => {
    const message = args.map(String).join(' ');
    // In real WASM, this would write to memory
  }
};

// Execute source code
try {
  const exports = {};
  const module = { exports };

  const require = (name) => {
    throw new Error('Module ' + name + ' not available in WebAssembly runtime');
  };

  const func = new Function('exports', 'require', 'module', 'globalThis', sourceCode);
  func(exports, require, module, globalThis);

  if (module.exports.default) {
    globalThis.main = module.exports.default;
  } else if (typeof module.exports === 'function') {
    globalThis.main = module.exports;
  } else {
    globalThis.main = () => JSON.stringify(module.exports);
  }
} catch (error) {
  globalThis.main = () => {
    throw new Error('Function execution failed: ' + error.message);
  };
}

// WASM exports
export function execute(input) {
  try {
    const result = globalThis.main(typeof input === 'string' ? JSON.parse(input) : input);
    return typeof result === 'string' ? result : JSON.stringify(result);
  } catch (error) {
    throw new Error('Execution failed: ' + error.message);
  }
}

export function init() {
  // WASM initialization
}
`, manifest.Name, sourceCode)

	return []byte(wrapper), nil
}
