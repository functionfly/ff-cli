package backend

import (
	"testing"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

func TestGenerateValue_Literals(t *testing.T) {
	tests := []struct {
		name     string
		value    ir.Value
		expected string
	}{
		{
			name: "string literal",
			value: ir.Value{
				Type:  ir.IRTypeString,
				Kind:  ir.Literal,
				Value: "hello",
			},
			expected: `"hello".to_string()`,
		},
		{
			name: "int literal",
			value: ir.Value{
				Type:  ir.IRTypeInt,
				Kind:  ir.Literal,
				Value: 42,
			},
			expected: `42`,
		},
		{
			name: "float literal",
			value: ir.Value{
				Type:  ir.IRTypeFloat,
				Kind:  ir.Literal,
				Value: 3.14,
			},
			expected: `3.14`,
		},
		{
			name: "bool true",
			value: ir.Value{
				Type:  ir.IRTypeBool,
				Kind:  ir.Literal,
				Value: true,
			},
			expected: `true`,
		},
		{
			name: "bool false",
			value: ir.Value{
				Type:  ir.IRTypeBool,
				Kind:  ir.Literal,
				Value: false,
			},
			expected: `false`,
		},
		{
			name: "none/null",
			value: ir.Value{
				Type:  ir.IRTypeNone,
				Kind:  ir.Literal,
				Value: nil,
			},
			expected: `serde_json::Value::Null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateValue(tt.value)
			if result != tt.expected {
				t.Errorf("GenerateValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGenerateValue_References(t *testing.T) {
	tests := []struct {
		name     string
		value    ir.Value
		expected string
	}{
		{
			name: "simple variable reference",
			value: ir.Value{
				Type:  ir.IRTypeUnknown,
				Kind:  ir.Reference,
				Value: "myVar",
			},
			expected: `myVar`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateValue(tt.value)
			if result != tt.expected {
				t.Errorf("GenerateValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGenerateValue_BinaryOps(t *testing.T) {
	tests := []struct {
		name     string
		value    ir.Value
		contains string
	}{
		{
			name: "addition",
			value: ir.Value{
				Type: ir.IRTypeInt,
				Kind: ir.BinOp,
				Value: map[string]interface{}{
					"op":    "Add",
					"left":  ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 1},
					"right": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 2},
				},
			},
			contains: "+",
		},
		{
			name: "subtraction",
			value: ir.Value{
				Type: ir.IRTypeInt,
				Kind: ir.BinOp,
				Value: map[string]interface{}{
					"op":    "Sub",
					"left":  ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 5},
					"right": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 3},
				},
			},
			contains: "-",
		},
		{
			name: "multiplication",
			value: ir.Value{
				Type: ir.IRTypeInt,
				Kind: ir.BinOp,
				Value: map[string]interface{}{
					"op":    "Mult",
					"left":  ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 4},
					"right": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 5},
				},
			},
			contains: "*",
		},
		{
			name: "division",
			value: ir.Value{
				Type: ir.IRTypeFloat,
				Kind: ir.BinOp,
				Value: map[string]interface{}{
					"op":    "Div",
					"left":  ir.Value{Type: ir.IRTypeFloat, Kind: ir.Literal, Value: 10.0},
					"right": ir.Value{Type: ir.IRTypeFloat, Kind: ir.Literal, Value: 2.0},
				},
			},
			contains: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateValue(tt.value)
			if !containsString(result, tt.contains) {
				t.Errorf("GenerateValue() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestGenerateValue_Lists(t *testing.T) {
	value := ir.Value{
		Type: ir.IRTypeList,
		Kind: ir.List,
		Value: map[string]interface{}{
			"elements": []ir.Value{
				{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 1},
				{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 2},
				{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 3},
			},
		},
	}

	result := GenerateValue(value)
	expected := "vec![1, 2, 3]"
	if result != expected {
		t.Errorf("GenerateValue() = %v, want %v", result, expected)
	}
}

func TestGenerateValue_Dicts(t *testing.T) {
	value := ir.Value{
		Type: ir.IRTypeDict,
		Kind: ir.Dict,
		Value: map[string]interface{}{
			"keys": []ir.Value{
				{Type: ir.IRTypeString, Kind: ir.Literal, Value: "name"},
			},
			"values": []ir.Value{
				{Type: ir.IRTypeString, Kind: ir.Literal, Value: "test"},
			},
		},
	}

	result := GenerateValue(value)
	if !containsString(result, "json!") {
		t.Errorf("GenerateValue() for dict should contain json! macro, got %v", result)
	}
}

func TestGenerateListComp(t *testing.T) {
	element := "x * 2"
	generators := []map[string]interface{}{
		{
			"target":     "x",
			"iterator":   ir.Value{Type: ir.IRTypeList, Kind: ir.Reference, Value: "items"},
			"conditions": []ir.Value{},
		},
	}

	result := GenerateListComp(element, generators)
	if !containsString(result, "iter()") {
		t.Errorf("GenerateListComp() should contain iter(), got %v", result)
	}
	if !containsString(result, "map") {
		t.Errorf("GenerateListComp() should contain map, got %v", result)
	}
	if !containsString(result, "collect") {
		t.Errorf("GenerateListComp() should contain collect, got %v", result)
	}
}

func TestGenerateListComp_WithFilter(t *testing.T) {
	element := "x"
	generators := []map[string]interface{}{
		{
			"target":   "x",
			"iterator": ir.Value{Type: ir.IRTypeList, Kind: ir.Reference, Value: "numbers"},
			"conditions": []ir.Value{
				{
					Type: ir.IRTypeBool,
					Kind: ir.Compare,
					Value: map[string]interface{}{
						"ops":         []string{"Gt"},
						"left":        ir.Value{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "x"},
						"comparators": []ir.Value{{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 0}},
					},
				},
			},
		},
	}

	result := GenerateListComp(element, generators)
	if !containsString(result, "filter") {
		t.Errorf("GenerateListComp() with filter should contain filter, got %v", result)
	}
}

func TestGenerateSlice(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		sliceVal ir.Value
		contains string
	}{
		{
			name:  "slice with lower and upper",
			value: "arr",
			sliceVal: ir.Value{
				Type: ir.IRTypeList,
				Kind: ir.Slice,
				Value: map[string]interface{}{
					"lower": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 1},
					"upper": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 3},
				},
			},
			contains: "[1..3]",
		},
		{
			name:  "slice with only upper",
			value: "arr",
			sliceVal: ir.Value{
				Type: ir.IRTypeList,
				Kind: ir.Slice,
				Value: map[string]interface{}{
					"upper": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 5},
				},
			},
			contains: "[..5]",
		},
		{
			name:  "slice with only lower",
			value: "arr",
			sliceVal: ir.Value{
				Type: ir.IRTypeList,
				Kind: ir.Slice,
				Value: map[string]interface{}{
					"lower": ir.Value{Type: ir.IRTypeInt, Kind: ir.Literal, Value: 2},
				},
			},
			contains: "[2..]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateSlice(tt.value, tt.sliceVal)
			if !containsString(result, tt.contains) {
				t.Errorf("GenerateSlice() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestPyOpToRustOp(t *testing.T) {
	tests := []struct {
		pyOp   string
		rustOp string
	}{
		{"Add", "+"},
		{"Sub", "-"},
		{"Mult", "*"},
		{"Div", "/"},
		{"Mod", "%"},
		{"Eq", "=="},
		{"NotEq", "!="},
		{"Lt", "<"},
		{"LtE", "<="},
		{"Gt", ">"},
		{"GtE", ">="},
	}

	for _, tt := range tests {
		t.Run(tt.pyOp, func(t *testing.T) {
			result := PyOpToRustOp(tt.pyOp)
			if result != tt.rustOp {
				t.Errorf("PyOpToRustOp(%v) = %v, want %v", tt.pyOp, result, tt.rustOp)
			}
		})
	}
}

func TestPyAugOpToRustOp(t *testing.T) {
	tests := []struct {
		pyOp   string
		rustOp string
	}{
		{"Add", "+"},
		{"Sub", "-"},
		{"Mult", "*"},
		{"Div", "/"},
		{"Mod", "%"},
	}

	for _, tt := range tests {
		t.Run(tt.pyOp, func(t *testing.T) {
			result := PyAugOpToRustOp(tt.pyOp)
			if result != tt.rustOp {
				t.Errorf("PyAugOpToRustOp(%v) = %v, want %v", tt.pyOp, result, tt.rustOp)
			}
		})
	}
}

func TestGenerateMethodCall(t *testing.T) {
	tests := []struct {
		name     string
		receiver string
		method   string
		args     []string
		contains string
	}{
		{
			name:     "string strip",
			receiver: "s",
			method:   "strip",
			args:     []string{},
			contains: "trim()",
		},
		{
			name:     "string upper",
			receiver: "s",
			method:   "upper",
			args:     []string{},
			contains: "to_uppercase()",
		},
		{
			name:     "string lower",
			receiver: "s",
			method:   "lower",
			args:     []string{},
			contains: "to_lowercase()",
		},
		{
			name:     "list append",
			receiver: "lst",
			method:   "append",
			args:     []string{"1"},
			contains: "push(1)",
		},
		{
			name:     "list pop",
			receiver: "lst",
			method:   "pop",
			args:     []string{},
			contains: "pop()",
		},
		{
			name:     "string replace",
			receiver: "s",
			method:   "replace",
			args:     []string{`"old"`, `"new"`},
			contains: "replace",
		},
		{
			name:     "string startswith",
			receiver: "s",
			method:   "startswith",
			args:     []string{`"prefix"`},
			contains: "starts_with",
		},
		{
			name:     "string endswith",
			receiver: "s",
			method:   "endswith",
			args:     []string{`"suffix"`},
			contains: "ends_with",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateMethodCall(tt.receiver, tt.method, tt.args)
			if !containsString(result, tt.contains) {
				t.Errorf("GenerateMethodCall() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestGenerateModuleCall(t *testing.T) {
	tests := []struct {
		name     string
		module   string
		fn       string
		args     []string
		contains string
	}{
		{
			name:     "json loads",
			module:   "json",
			fn:       "loads",
			args:     []string{`"{}"`},
			contains: "json_loads",
		},
		{
			name:     "json dumps",
			module:   "json",
			fn:       "dumps",
			args:     []string{"obj"},
			contains: "serde_json::to_string",
		},
		{
			name:     "re match",
			module:   "re",
			fn:       "match",
			args:     []string{`"pattern"`, `"text"`},
			contains: "re_match",
		},
		{
			name:     "re search",
			module:   "re",
			fn:       "search",
			args:     []string{`"pattern"`, `"text"`},
			contains: "re_search",
		},
		{
			name:     "re sub",
			module:   "re",
			fn:       "sub",
			args:     []string{`"pattern"`, `"repl"`, `"text"`},
			contains: "re_sub",
		},
		{
			name:     "re findall",
			module:   "re",
			fn:       "findall",
			args:     []string{`"pattern"`, `"text"`},
			contains: "re_findall",
		},
		{
			name:     "csv reader",
			module:   "csv",
			fn:       "reader",
			args:     []string{`"data"`},
			contains: "csv_reader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateModuleCall(tt.module, tt.fn, tt.args)
			if !containsString(result, tt.contains) {
				t.Errorf("GenerateModuleCall() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestGenerateValue_Isinstance(t *testing.T) {
	tests := []struct {
		name     string
		value    ir.Value
		contains string
	}{
		{
			name: "isinstance list check",
			value: ir.Value{
				Type: ir.IRTypeBool,
				Kind: ir.Call,
				Value: map[string]interface{}{
					"func": "isinstance",
					"args": []ir.Value{
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "event"},
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "list"},
					},
				},
			},
			contains: ".is_array()",
		},
		{
			name: "isinstance dict check",
			value: ir.Value{
				Type: ir.IRTypeBool,
				Kind: ir.Call,
				Value: map[string]interface{}{
					"func": "isinstance",
					"args": []ir.Value{
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "data"},
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "dict"},
					},
				},
			},
			contains: ".is_object()",
		},
		{
			name: "isinstance str check",
			value: ir.Value{
				Type: ir.IRTypeBool,
				Kind: ir.Call,
				Value: map[string]interface{}{
					"func": "isinstance",
					"args": []ir.Value{
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "name"},
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "str"},
					},
				},
			},
			contains: ".is_string()",
		},
		{
			name: "isinstance int check",
			value: ir.Value{
				Type: ir.IRTypeBool,
				Kind: ir.Call,
				Value: map[string]interface{}{
					"func": "isinstance",
					"args": []ir.Value{
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "count"},
						{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "int"},
					},
				},
			},
			contains: ".is_i64()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateValue(tt.value)
			if !containsString(result, tt.contains) {
				t.Errorf("GenerateValue() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestExtractTypeName(t *testing.T) {
	tests := []struct {
		name     string
		value    ir.Value
		expected string
	}{
		{
			name:     "reference to list",
			value:    ir.Value{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "list"},
			expected: "list",
		},
		{
			name:     "reference to dict",
			value:    ir.Value{Type: ir.IRTypeUnknown, Kind: ir.Reference, Value: "dict"},
			expected: "dict",
		},
		{
			name:     "string literal",
			value:    ir.Value{Type: ir.IRTypeString, Kind: ir.Literal, Value: "str"},
			expected: "str",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTypeName(tt.value)
			if result != tt.expected {
				t.Errorf("extractTypeName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
