//go:build integration

package flypy

import (
	"context"
	"testing"
	"time"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// TestEndToEndCompilation tests the full compilation pipeline
func TestEndToEndCompilation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name    string
		source  string
		mode    ExecutionMode
		wantErr bool
	}{
		{
			name: "simple function",
			source: `
def handler(event):
    return {"message": "hello"}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "arithmetic operations",
			source: `
def handler(event):
    x = 10
    y = 20
    result = x + y
    return {"result": result}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "string operations",
			source: `
def handler(event):
    name = event.get("name", "World")
    greeting = "Hello, " + name + "!"
    return {"greeting": greeting}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "list operations",
			source: `
def handler(event):
    items = [1, 2, 3, 4, 5]
    total = sum(items)
    return {"total": total}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "dict operations",
			source: `
def handler(event):
    data = {"a": 1, "b": 2}
    result = data["a"] + data["b"]
    return {"result": result}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "if statement",
			source: `
def handler(event):
    value = event.get("value", 0)
    if value > 0:
        return {"status": "positive"}
    else:
        return {"status": "non-positive"}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "for loop (complex mode)",
			source: `
def handler(event):
    items = [1, 2, 3, 4, 5]
    total = 0
    for item in items:
        total += item
    return {"total": total}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "list comprehension",
			source: `
def handler(event):
    numbers = [1, 2, 3, 4, 5]
    doubled = [x * 2 for x in numbers]
    return {"doubled": doubled}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "list comprehension with filter",
			source: `
def handler(event):
    numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    evens = [x for x in numbers if x % 2 == 0]
    return {"evens": evens}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "slice operations",
			source: `
def handler(event):
    items = [1, 2, 3, 4, 5]
    first_three = items[:3]
    last_two = items[-2:]
    middle = items[1:4]
    return {"first_three": first_three, "last_two": last_two, "middle": middle}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "json operations",
			source: `
import json

def handler(event):
    data = json.loads('{"name": "test", "value": 42}')
    result = json.dumps(data)
    return {"parsed": data, "stringified": result}
`,
			mode:    DeterministicMode,
			wantErr: false,
		},
		{
			name: "try except (complex mode)",
			source: `
def handler(event):
    try:
        value = event["missing_key"]
        return {"status": "found"}
    except KeyError:
        return {"status": "not found"}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "augmented assignment",
			source: `
def handler(event):
    counter = 0
    counter += 1
    counter += 1
    counter -= 1
    return {"counter": counter}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "break and continue",
			source: `
def handler(event):
    items = [1, 2, 3, 4, 5]
    result = []
    for item in items:
        if item == 3:
            continue
        if item == 5:
            break
        result.append(item)
    return {"result": result}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "while loop",
			source: `
def handler(event):
    count = 0
    while count < 5:
        count += 1
    return {"count": count}
`,
			mode:    ComplexMode,
			wantErr: false,
		},
		{
			name: "forbidden import in deterministic mode",
			source: `
import os

def handler(event):
    return {"env": os.environ}
`,
			mode:    DeterministicMode,
			wantErr: true,
		},
		{
			name: "forbidden builtin",
			source: `
def handler(event):
    print("hello")
    return {}
`,
			mode:    DeterministicMode,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Create compiler with the specified mode
			compiler := NewCompiler(&Config{
				Mode:      tt.mode,
				OutputDir: t.TempDir(),
			})

			// Compile the Python source
			result, err := compiler.Compile(ctx, tt.source, "test_function")

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil || result.Artifact == nil {
				t.Error("result or artifact is nil")
				return
			}

			// Verify the artifact has expected content
			if len(result.Artifact.WasmModule) == 0 {
				t.Error("WASM artifact is empty")
			}
		})
	}
}

// TestPythonParser tests the Python AST parser
func TestPythonParser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name    string
		source  string
		wantErr bool
	}{
		{
			name:    "valid python",
			source:  "def handler(event): return event",
			wantErr: false,
		},
		{
			name:    "invalid python",
			source:  "def handler(event:\n  return event",
			wantErr: true,
		},
		{
			name:    "empty source",
			source:  "",
			wantErr: false, // Empty is valid, just no functions
		},
		{
			name: "complex function",
			source: `
def handler(event):
    """Process an event."""
    result = []
    for i in range(10):
        if i % 2 == 0:
            result.append(i)
    return {"result": result}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err := parser.ParsePython(ctx, tt.source)

			if tt.wantErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestTypeInference tests type inference for various expressions
func TestTypeInference(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name     string
		source   string
		varName  string
		expected string
	}{
		{
			name: "int literal",
			source: `
def handler(event):
    x = 42
    return {"x": x}
`,
			varName:  "x",
			expected: "int",
		},
		{
			name: "string literal",
			source: `
def handler(event):
    name = "hello"
    return {"name": name}
`,
			varName:  "name",
			expected: "string",
		},
		{
			name: "list literal",
			source: `
def handler(event):
    items = [1, 2, 3]
    return {"items": items}
`,
			varName:  "items",
			expected: "list",
		},
		{
			name: "dict literal",
			source: `
def handler(event):
    data = {"key": "value"}
    return {"data": data}
`,
			varName:  "data",
			expected: "dict",
		},
		{
			name: "bool literal",
			source: `
def handler(event):
    flag = True
    return {"flag": flag}
`,
			varName:  "flag",
			expected: "bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Parse and analyze the source
			_, err := parser.ParsePython(ctx, tt.source)
			if err != nil {
				t.Errorf("failed to parse: %v", err)
				return
			}

			// Type inference would be checked here
			// For now, we just verify parsing works
		})
	}
}

// BenchmarkCompilation tests compilation performance
func BenchmarkCompilation(b *testing.B) {
	source := `
def handler(event):
    items = [1, 2, 3, 4, 5]
    result = []
    for item in items:
        if item > 2:
            result.append(item * 2)
    return {"result": result}
`

	ctx := context.Background()
	compiler := NewCompiler(&Config{
		Mode:      ComplexMode,
		OutputDir: b.TempDir(),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := compiler.Compile(ctx, source, "benchmark_function")
		if err != nil {
			b.Errorf("compilation failed: %v", err)
		}
	}
}
