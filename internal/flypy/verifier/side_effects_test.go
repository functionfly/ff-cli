package verifier

import (
	"testing"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

func TestSideEffectAnalyzer_Analyze(t *testing.T) {
	// Create a simple IR module with a function that has side effects
	module := &ir.Module{
		Name: "test",
		Functions: []*ir.Function{
			{
				Name: "test_function",
				Parameters: []ir.Parameter{
					{Name: "x", Type: ir.IRTypeInt},
				},
				Body: []ir.Operation{
					// Call to print (I/O side effect)
					{
						Type:   "call",
						Result: "",
						Operands: []ir.Value{
							{Type: ir.IRTypeString, Kind: ir.Literal, Value: "hello"},
						},
						Kind: ir.Call,
						Value: map[string]interface{}{
							"func": "print",
							"args": []ir.Value{
								{Type: ir.IRTypeString, Kind: ir.Literal, Value: "hello"},
							},
						},
					},
					// Assignment to parameter (mutation side effect)
					{
						Type:   "assign",
						Result: "x",
						Operands: []ir.Value{
							{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 42},
						},
						Type_: ir.IRTypeInt,
					},
				},
				ReturnType: ir.IRTypeNone,
				Pure:       false,
			},
		},
	}

	analyzer := NewSideEffectAnalyzer(module)
	effects := analyzer.Analyze()

	// Should detect 2 side effects: I/O and mutation
	if len(effects) != 2 {
		t.Errorf("Expected 2 side effects, got %d", len(effects))
	}

	// Check for I/O side effect
	foundIO := false
	foundMutation := false
	for _, effect := range effects {
		if effect.Type == SideEffectIO && effect.Target == "print" {
			foundIO = true
		}
		if effect.Type == SideEffectMutation && effect.Target == "x" {
			foundMutation = true
		}
	}

	if !foundIO {
		t.Error("Expected to find I/O side effect for print call")
	}
	if !foundMutation {
		t.Error("Expected to find mutation side effect for parameter assignment")
	}
}

func TestSideEffectAnalyzer_IsIdempotent(t *testing.T) {
	// Test pure function (no side effects)
	pureModule := &ir.Module{
		Name: "pure",
		Functions: []*ir.Function{
			{
				Name: "pure_func",
				Parameters: []ir.Parameter{
					{Name: "x", Type: ir.IRTypeInt},
				},
				Body: []ir.Operation{
					// Simple arithmetic (no side effects)
					{
						Type:   "add",
						Result: "result",
						Operands: []ir.Value{
							{Type: ir.IRTypeInt, Kind: ir.Reference, Value: "x"},
							{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 1},
						},
						Type_: ir.IRTypeInt,
					},
				},
				ReturnType: ir.IRTypeInt,
				Pure:       true,
			},
		},
	}

	analyzer := NewSideEffectAnalyzer(pureModule)
	_ = analyzer.Analyze()

	if !analyzer.IsIdempotent(pureModule.Functions[0]) {
		t.Error("Expected pure function to be idempotent")
	}

	// Test function with external side effects
	impureModule := &ir.Module{
		Name: "impure",
		Functions: []*ir.Function{
			{
				Name: "impure_func",
				Body: []ir.Operation{
					// Call to time.time() (external state)
					{
						Type:     "call",
						Result:   "",
						Operands: []ir.Value{},
						Kind:     ir.Call,
						Value: map[string]interface{}{
							"func": "time.time",
							"args": []ir.Value{},
						},
					},
				},
				ReturnType: ir.IRTypeFloat,
				Pure:       false,
			},
		},
	}

	analyzer2 := NewSideEffectAnalyzer(impureModule)
	_ = analyzer2.Analyze()

	if analyzer2.IsIdempotent(impureModule.Functions[0]) {
		t.Error("Expected function with external state access to not be idempotent")
	}
}

func TestSideEffectAnalyzer_GetSideEffectSummary(t *testing.T) {
	module := &ir.Module{
		Name: "summary_test",
		Functions: []*ir.Function{
			{
				Name: "test_func",
				Body: []ir.Operation{
					// I/O operation
					{
						Type:     "call",
						Result:   "",
						Operands: []ir.Value{},
						Kind:     ir.Call,
						Value: map[string]interface{}{
							"func": "print",
							"args": []ir.Value{},
						},
					},
					// Network operation
					{
						Type:     "call",
						Result:   "",
						Operands: []ir.Value{},
						Kind:     ir.Call,
						Value: map[string]interface{}{
							"func": "requests.get",
							"args": []ir.Value{},
						},
					},
				},
			},
		},
	}

	analyzer := NewSideEffectAnalyzer(module)
	_ = analyzer.Analyze()

	summary := analyzer.GetSideEffectSummary()

	if summary[SideEffectIO] != 1 {
		t.Errorf("Expected 1 I/O side effect, got %d", summary[SideEffectIO])
	}
	if summary[SideEffectNetwork] != 1 {
		t.Errorf("Expected 1 network side effect, got %d", summary[SideEffectNetwork])
	}
}
