package backend

import (
	"fmt"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

type Field struct {
	Name         string
	Type         string
	DefaultValue string
}

func GoTypeToRust(t ir.IRType) string {
	// Input parameters should use serde_json::Value
	if t.IsInput {
		return "serde_json::Value"
	}

	// Computed results use concrete types
	switch t.Base {
	case "int":
		return "i32"
	case "float":
		return "f64"
	case "string":
		return "String"
	case "bool":
		return "bool"
	case "list":
		if t.Element != nil {
			elementType := GoTypeToRust(*t.Element)
			return fmt.Sprintf("Vec<%s>", elementType)
		}
		return "Vec<String>" // Default to Vec<String>
	case "dict":
		if t.Element != nil {
			elementType := GoTypeToRust(*t.Element)
			return fmt.Sprintf("std::collections::HashMap<String, %s>", elementType)
		}
		return "std::collections::HashMap<String, String>" // Default to HashMap<String, String>
	case "none":
		return "()"
	case "unknown":
		return "serde_json::Value"
	default:
		return "serde_json::Value"
	}
}

func GetDefaultValue(t ir.IRType) string {
	// Input parameters use serde_json::Value defaults
	if t.IsInput {
		return "serde_json::Value::Null"
	}

	// Computed results use concrete type defaults
	switch t.Base {
	case "int":
		return "0"
	case "float":
		return "0.0"
	case "string":
		return "String::new()"
	case "bool":
		return "false"
	case "list":
		return "Vec::new()"
	case "dict":
		return "std::collections::HashMap::new()"
	case "none":
		return "()"
	default:
		return "serde_json::Value::Null"
	}
}

func PyOpToRustOp(op string) string {
	switch op {
	case "Add":
		return "+"
	case "Sub":
		return "-"
	case "Mult":
		return "*"
	case "Div":
		return "/"
	case "Mod":
		return "%"
	case "Pow":
		return ".powf()"
	case "Eq":
		return "=="
	case "NotEq":
		return "!="
	case "Lt":
		return "<"
	case "LtE":
		return "<="
	case "Gt":
		return ">"
	case "GtE":
		return ">="
	case "In":
		// "In" is handled specially in GenerateValue for Compare
		return "in"
	case "NotIn":
		return "not in"
	default:
		return op
	}
}

// PyAugOpToRustOp converts Python augmented assignment operators to Rust
func PyAugOpToRustOp(op string) string {
	switch op {
	case "Add":
		return "+"
	case "Sub":
		return "-"
	case "Mult":
		return "*"
	case "Div":
		return "/"
	case "Mod":
		return "%"
	default:
		return ""
	}
}
