package restrictions

import (
	"context"
	"testing"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

func TestComplexModeAllowedModules(t *testing.T) {
	tests := []struct {
		module    string
		mode      ExecutionMode
		shouldErr bool
	}{
		// Deterministic mode - minimal modules
		{"json", ModeDeterministic, false},
		{"math", ModeDeterministic, false},
		{"typing", ModeDeterministic, false},
		{"collections", ModeDeterministic, false},
		{"csv", ModeDeterministic, true},      // Not allowed in deterministic
		{"io", ModeDeterministic, true},       // Not allowed in deterministic
		{"re", ModeDeterministic, true},       // Not allowed in deterministic
		{"datetime", ModeDeterministic, true}, // Not allowed in deterministic

		// Complex mode - extended modules
		{"json", ModeComplex, false},
		{"math", ModeComplex, false},
		{"typing", ModeComplex, false},
		{"collections", ModeComplex, false},
		{"csv", ModeComplex, false},       // Allowed in complex
		{"io", ModeComplex, false},        // Allowed in complex
		{"re", ModeComplex, false},        // Allowed in complex
		{"datetime", ModeComplex, false},  // Allowed in complex
		{"itertools", ModeComplex, false}, // Allowed in complex
		{"functools", ModeComplex, false}, // Allowed in complex
		{"hashlib", ModeComplex, false},   // Allowed in complex
		{"base64", ModeComplex, false},    // Allowed in complex
		{"os", ModeComplex, true},         // Never allowed
		{"sys", ModeComplex, true},        // Never allowed
		{"random", ModeComplex, true},     // Never allowed

		// Compatible mode - all modules allowed (nil whitelist)
		{"json", ModeCompatible, false},
		{"csv", ModeCompatible, false},
		{"os", ModeCompatible, false},
		{"random", ModeCompatible, false},
		{"sys", ModeCompatible, false},
		{"open", ModeCompatible, false},
		{"eval", ModeCompatible, false},
		{"exec", ModeCompatible, false},
		{"print", ModeCompatible, false},
		{"input", ModeCompatible, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode)+"_"+tt.module, func(t *testing.T) {
			allowedModules := GetAllowedModules(tt.mode)

			if tt.mode == ModeCompatible {
				if allowedModules != nil {
					t.Errorf("Expected nil allowedModules for compatible mode, got non-nil")
				}
				if tt.shouldErr {
					t.Errorf("module %s in %s mode: expected shouldErr=%v, got false (nil whitelist)", tt.module, tt.mode, tt.shouldErr)
				}
				return
			}

			if allowedModules == nil {
				t.Fatalf("GetAllowedModules returned nil for mode %s", tt.mode)
			}

			_, allowed := allowedModules[tt.module]
			hasError := !allowed

			if hasError != tt.shouldErr {
				t.Errorf("module %s in %s mode: expected shouldErr=%v, got %v",
					tt.module, tt.mode, tt.shouldErr, hasError)
			}
		})
	}
}

func TestEnforceWithModeComplex(t *testing.T) {
	// Test that complex mode allows csv import
	pythonCode := `
import csv
import io
import re

def handler(data):
    output = io.StringIO()
    writer = csv.writer(output)
    writer.writerow(['a', 'b', 'c'])
    return {"result": output.getvalue()}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should pass in complex mode (csv, io, re are allowed)
	errors := EnforceWithMode(ast, ModeComplex)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in complex mode, got: %v", errors)
	}
}

func TestEnforceWithModeLoops(t *testing.T) {
	// Test that for loops are allowed in complex mode
	pythonCode := `
def handler(data):
    result = []
    for item in data:
        result.append(item)
    return {"result": result}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should pass in complex mode (for loops are allowed)
	errors := EnforceWithMode(ast, ModeComplex)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in complex mode for for loops, got: %v", errors)
	}
}

func TestEnforceWithModeRegex(t *testing.T) {
	// Test that re module is allowed in complex mode
	pythonCode := `
import re

def handler(data):
    pattern = r'\d+'
    matches = re.findall(pattern, data.get("text", ""))
    return {"matches": matches}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should pass in complex mode (re is allowed)
	errors := EnforceWithMode(ast, ModeComplex)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in complex mode for re import, got: %v", errors)
	}
}

func TestEnforceWithModeDatetime(t *testing.T) {
	// Test that datetime module is allowed in complex mode
	pythonCode := `
from datetime import datetime, timedelta

def handler(data):
    now = datetime.now()
    future = now + timedelta(days=7)
    return {"future": future.isoformat()}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should pass in complex mode (datetime is allowed)
	errors := EnforceWithMode(ast, ModeComplex)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in complex mode for datetime import, got: %v", errors)
	}
}

func TestForbiddenModulesEvenInComplexMode(t *testing.T) {
	// Test that some modules are never allowed even in complex mode
	// Note: os and random are in ForbiddenBuiltins, not just restricted imports
	pythonCode := `
import os

def handler(data):
    return {"result": "test"}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should fail in complex mode (os is not in ComplexModules allowlist)
	errors := EnforceWithMode(ast, ModeComplex)
	if len(errors) == 0 {
		t.Error("Expected errors in complex mode for os import")
	}
}

// Test that for loops are now allowed in deterministic mode
func TestForLoopsInDeterministicMode(t *testing.T) {
	pythonCode := `
def handler(data):
    result = []
    for item in data:
        result.append(item)
    return {"result": result}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should now pass in deterministic mode too
	errors := EnforceWithMode(ast, ModeDeterministic)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in deterministic mode for for loops, got: %v", errors)
	}
}

// Test that while loops are now allowed in deterministic mode
func TestWhileLoopsInDeterministicMode(t *testing.T) {
	pythonCode := `
def handler(data):
    count = 0
    while count < 5:
        count = count + 1
    return {"count": count}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should now pass in deterministic mode too
	errors := EnforceWithMode(ast, ModeDeterministic)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in deterministic mode for while loops, got: %v", errors)
	}
}

// Test that list comprehensions are now allowed in deterministic mode
func TestListComprehensionsInDeterministicMode(t *testing.T) {
	pythonCode := `
def handler(data):
    result = [x * 2 for x in data if x > 0]
    return {"result": result}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should now pass in deterministic mode too
	errors := EnforceWithMode(ast, ModeDeterministic)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in deterministic mode for list comprehensions, got: %v", errors)
	}
}

// Test that try/except blocks are now allowed in deterministic mode
func TestTryExceptInDeterministicMode(t *testing.T) {
	pythonCode := `
def handler(data):
    try:
        result = data["key"]
    except KeyError:
        result = "default"
    except Exception as e:
        result = str(e)
    finally:
        pass
    return {"result": result}
`
	ast, err := parser.ParsePython(context.Background(), pythonCode)
	if err != nil {
		t.Fatalf("Failed to parse Python: %v", err)
	}

	// Should now pass in deterministic mode too
	errors := EnforceWithMode(ast, ModeDeterministic)
	if len(errors) > 0 {
		t.Errorf("Expected no errors in deterministic mode for try/except, got: %v", errors)
	}
}
