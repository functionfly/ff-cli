package ir

import (
	"encoding/json"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// inferType infers the IRType from a Go value
func inferType(val interface{}) IRType {
	switch v := val.(type) {
	case int, int64:
		return IRTypeInt
	case float64:
		return IRTypeFloat
	case string:
		return IRTypeString
	case bool:
		return IRTypeBool
	case nil:
		return IRTypeNone
	case []interface{}:
		// Try to infer element type from the first element
		elementType := IRTypeString // Default to string
		if len(v) > 0 {
			elementType = inferType(v[0])
		}
		return IRType{Base: "list", Element: &elementType}
	case map[string]interface{}:
		// Try to infer value type from the first value
		elementType := IRTypeString // Default to string
		for _, val := range v {
			elementType = inferType(val)
			break
		}
		return IRType{Base: "dict", Key: &IRTypeString, Element: &elementType}
	default:
		// Try to handle JSON number types from parsing
		if _, ok := v.(json.Number); ok {
			// Could be int or float, default to float for safety
			return IRTypeFloat
		}
		return IRTypeUnknown
	}
}

// inferModuleCallReturnType infers the return type of a module function call
func inferModuleCallReturnType(module, funcName string) IRType {
	switch module {
	case "csv":
		switch funcName {
		case "reader":
			// csv.reader returns an iterator of lists of strings
			elementType := IRType{Base: "list", Element: &IRTypeString}
			return IRType{Base: "list", Element: &elementType}
		case "DictReader":
			// DictReader returns an iterator of dicts
			elementType := IRType{Base: "dict", Key: &IRTypeString, Element: &IRTypeString}
			return IRType{Base: "list", Element: &elementType}
		}
	case "io":
		switch funcName {
		case "StringIO":
			return IRTypeString // StringIO acts like a string
		case "BytesIO":
			return IRType{Base: "bytes"}
		}
	case "json":
		switch funcName {
		case "loads":
			return IRTypeUnknown // JSON parsing returns unknown type
		case "dumps":
			return IRTypeString
		}
	case "re":
		switch funcName {
		case "match", "search", "findall", "split":
			return IRType{Base: "list", Element: &IRTypeString}
		case "compile":
			return IRTypeUnknown // Compiled regex
		}
	}
	return IRTypeUnknown
}

// inferTypeFromExpr attempts to infer the type from an expression
func inferTypeFromExpr(expr map[string]interface{}) IRType {
	// Check for constant/literal
	if parser.IsConstant(expr) {
		val := parser.GetConstantValue(expr)
		return inferType(val)
	}

	// Check for list literal
	if parser.IsList(expr) {
		return IRTypeList
	}

	// Check for dict literal
	if parser.IsDict(expr) {
		return IRTypeDict
	}

	// Check for list comprehension
	if parser.IsListComp(expr) {
		return IRTypeList
	}

	// Check for dict comprehension
	if parser.IsDictComp(expr) {
		return IRTypeDict
	}

	// Check for comparison (always returns bool)
	if parser.IsCompare(expr) {
		return IRTypeBool
	}

	// Check for boolean operation (always returns bool)
	if parser.IsBoolOp(expr) {
		return IRTypeBool
	}

	// Check for binary operation - depends on operands
	if parser.IsBinOp(expr) {
		_, left, right := parser.GetBinOpInfo(expr)
		leftType := IRTypeUnknown
		rightType := IRTypeUnknown
		if leftMap, ok := left.(map[string]interface{}); ok {
			leftType = inferTypeFromExpr(leftMap)
		}
		if rightMap, ok := right.(map[string]interface{}); ok {
			rightType = inferTypeFromExpr(rightMap)
		}
		// If either operand is float, result is float
		if leftType.Base == "float" || rightType.Base == "float" {
			return IRTypeFloat
		}
		// If both are int, result is int
		if leftType.Base == "int" && rightType.Base == "int" {
			return IRTypeInt
		}
		// String concatenation
		if leftType.Base == "string" || rightType.Base == "string" {
			return IRTypeString
		}
	}

	// Check for unary operation
	if parser.IsUnaryOp(expr) {
		operand := parser.GetUnaryOpOperand(expr)
		if operandMap, ok := operand.(map[string]interface{}); ok {
			return inferTypeFromExpr(operandMap)
		}
	}

	// Check for call - try to infer from known functions
	if parser.IsCall(expr) {
		funcName := parser.GetCallFunc(expr)
		switch funcName {
		case "len":
			return IRTypeInt
		case "str":
			return IRTypeString
		case "int":
			return IRTypeInt
		case "float":
			return IRTypeFloat
		case "bool":
			return IRTypeBool
		case "list":
			return IRTypeList
		case "dict":
			return IRTypeDict
		case "range":
			return IRTypeList // range returns an iterable
		}
	}

	return IRTypeUnknown
}

// KnownModules is a set of Python module names that we support in complex mode
var KnownModules = map[string]bool{
	"csv":       true,
	"io":        true,
	"re":        true,
	"json":      true,
	"datetime":  true,
	"hashlib":   true,
	"base64":    true,
	"math":      true,
	"itertools": true,
	"functools": true,
	"operator":  true,
	"string":    true,
	"textwrap":  true,
	"uuid":      true,
}

// splitModuleFunction checks if a name is a known module and splits it
// Returns (module, function) if it's a module function, ("", "") otherwise
func splitModuleFunction(name string) (string, string) {
	// Check for dotted names like "csv.reader"
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			module := name[:i]
			if KnownModules[module] {
				return module, name[i+1:]
			}
		}
	}
	return "", ""
}

// isKnownModule checks if a name is a known Python module
func isKnownModule(name string) bool {
	return KnownModules[name]
}

// convertSliceExpression converts a Python slice expression to IR
// Handles: arr[1:3], arr[:5], arr[2:], arr[::2], arr[1:5:2]
func convertSliceExpression(sliceMap map[string]interface{}) (Value, IRType, error) {
	lower := parser.GetSliceLower(sliceMap)
	upper := parser.GetSliceUpper(sliceMap)
	step := parser.GetSliceStep(sliceMap)

	sliceInfo := make(map[string]interface{})

	// Convert lower bound
	if lower != nil {
		if lowerMap, ok := lower.(map[string]interface{}); ok {
			lowerVal, _, err := convertExpression(lowerMap)
			if err != nil {
				return Value{}, IRTypeUnknown, err
			}
			sliceInfo["lower"] = lowerVal
		}
	}

	// Convert upper bound
	if upper != nil {
		if upperMap, ok := upper.(map[string]interface{}); ok {
			upperVal, _, err := convertExpression(upperMap)
			if err != nil {
				return Value{}, IRTypeUnknown, err
			}
			sliceInfo["upper"] = upperVal
		}
	}

	// Convert step
	if step != nil {
		if stepMap, ok := step.(map[string]interface{}); ok {
			stepVal, _, err := convertExpression(stepMap)
			if err != nil {
				return Value{}, IRTypeUnknown, err
			}
			sliceInfo["step"] = stepVal
		}
	}

	return Value{
		Type:  IRTypeList, // Slices return lists
		Kind:  Slice,
		Value: sliceInfo,
	}, IRTypeList, nil
}
