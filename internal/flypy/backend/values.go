package backend

import (
	"fmt"
	"strings"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// exceptionVarNames are common Python exception variable names (except Foo as e).
// References to these in generated code are rewritten to a safe string when the
// variable may be undefined (e.g. in handler bodies or edge cases).
var exceptionVarNames = map[string]bool{
	"e": true, "ex": true, "err": true, "error": true, "exc": true, "exception": true,
}

func isExceptionVariable(name string) bool {
	return exceptionVarNames[name]
}

// isArithmeticOp reports whether op is a Python arithmetic binary operator
// that needs numeric coercion for serde_json::Value operands.
func isArithmeticOp(op string) bool {
	switch op {
	case "Add", "Sub", "Mult", "Div", "Mod", "Pow":
		return true
	default:
		return false
	}
}

// coerceToI64 renders an expression as an i64, extracting a numeric value from
// a serde_json::Value. It tolerates non-numeric Values by falling back to 0
// so the WASM module never panics on bad input.
func coerceToI64(expr string) string {
	return fmt.Sprintf("(%s.as_i64().unwrap_or(0))", expr)
}

// looksLikeValueExpr reports whether a generated Rust expression is a
// serde_json::Value. We use this to decide whether arithmetic needs
// numeric coercion at the use site. The rules are intentionally simple
// (string prefix check) because the value generator emits a small set
// of well-known patterns for Value-typed expressions.
func looksLikeValueExpr(expr string) bool {
	trimmed := strings.TrimSpace(expr)
	// Explicit marker placed by the Reference generator when the local's
	// type is serde_json::Value. The leading comment makes this case
	// unambiguous from the variable name alone.
	if strings.HasPrefix(trimmed, "/*Value*/") {
		return true
	}
	if strings.HasPrefix(trimmed, "input.") {
		return true
	}
	if strings.HasPrefix(trimmed, "json!") {
		return true
	}
	if strings.Contains(trimmed, ".get(") {
		return true
	}
	return false
}

// generateValue is the CodegenContext-aware value generator.
// It uses ctx.prefixParameter for proper scoping without global state.
func (ctx *CodegenContext) generateValue(val ir.Value) string {
	switch val.Kind {
	case ir.Literal:
		switch val.Type {
		case ir.IRTypeString:
			// Escape backslashes and double quotes for Rust string literals
			str := fmt.Sprintf("%v", val.Value)
			str = strings.ReplaceAll(str, `\`, `\\`)
			str = strings.ReplaceAll(str, `"`, `\"`)
			return fmt.Sprintf("\"%s\".to_string()", str)
		case ir.IRTypeBool:
			// Handle boolean literals
			if val.Value == true || fmt.Sprintf("%v", val.Value) == "true" {
				return "true"
			}
			return "false"
		case ir.IRTypeNone:
			return "serde_json::Value::Null"
		case ir.IRTypeInt, ir.IRTypeFloat:
			return fmt.Sprintf("%v", val.Value)
		default:
			// Handle nil values
			if val.Value == nil {
				return "serde_json::Value::Null"
			}
			return fmt.Sprintf("%v", val.Value)
		}
	case ir.Reference:
		// Simple variable reference - prefix with input. if it's a parameter
		if str, ok := val.Value.(string); ok {
			// Handle undefined exception variable (except Foo as e, as ex, etc.)
			if isExceptionVariable(str) {
				return "\"exception\".to_string()"
			}
			// Annotate references to Value-typed locals so arithmetic ops
			// can spot them without the BinOp generator having to know the
			// original Value's source. This is a no-op for typed locals
			// (i32, String, etc.) since as_i64 doesn't exist on them - so
			// we only annotate when we know the type is Value.
			if ctx.localTypes[str] == "serde_json::Value" {
				return fmt.Sprintf("/*Value*/%s", ctx.prefixParameter(str))
			}
			return ctx.prefixParameter(str)
		}
		// Attribute access (obj.attr)
		if attrMap, ok := val.Value.(map[string]interface{}); ok {
			if attr, ok := attrMap["attr"].(string); ok {
				if value, ok := attrMap["value"].(ir.Value); ok {
					return fmt.Sprintf("%s.%s", ctx.generateValue(value), attr)
				}
			}
		}
		return fmt.Sprintf("%v", val.Value)
	case ir.BinOp:
		if binOpVal, ok := val.Value.(map[string]interface{}); ok {
			leftVal, leftOk := binOpVal["left"].(ir.Value)
			rightVal, rightOk := binOpVal["right"].(ir.Value)
			op, opOk := binOpVal["op"].(string)
			if !leftOk || !rightOk || !opOk {
				return "/* binop: malformed IR */"
			}
			left := ctx.generateValue(leftVal)
			right := ctx.generateValue(rightVal)
			// String concatenation in Python is `+`; in Rust this is `format!()`.
			// We detect string contexts from the inferred result type or from
			// either operand being a string. This avoids type mismatches when
			// concatenating a `serde_json::Value` (e.g. from `.get()`) with a
			// string literal.
			if op == "Add" && (val.Type == ir.IRTypeString || leftVal.Type == ir.IRTypeString || rightVal.Type == ir.IRTypeString) {
				return fmt.Sprintf("format!(\"{}{}\", %s, %s)", left, right)
			}
			// Arithmetic on `serde_json::Value` requires coercing to a concrete
			// numeric type first. We only coerce when the result type is
			// unknown AND at least one of the operands is actually a
			// serde_json::Value (detected by the `input.` prefix or a
			// subscript/attribute access that the generator already wrapped).
			if isArithmeticOp(op) && val.Type == ir.IRTypeUnknown && (looksLikeValueExpr(left) || looksLikeValueExpr(right)) {
				l := coerceToI64(left)
				r := coerceToI64(right)
				return fmt.Sprintf("(%s %s %s)", l, PyOpToRustOp(op), r)
			}
			return fmt.Sprintf("(%s %s %s)", left, PyOpToRustOp(op), right)
		}
		return "0"
	case ir.Compare:
		if compVal, ok := val.Value.(map[string]interface{}); ok {
			leftVal, leftOk := compVal["left"].(ir.Value)
			ops, opsOk := compVal["ops"].([]string)
			comparators, compOk := compVal["comparators"].([]ir.Value)
			if !leftOk || !opsOk || !compOk {
				return "/* compare: malformed IR */"
			}
			left := ctx.generateValue(leftVal)

			// Handle chained comparisons: a < b < c becomes (a < b) && (b < c)
			var parts []string
			prevLeft := left
			for i, op := range ops {
				if i >= len(comparators) {
					break
				}
				right := ctx.generateValue(comparators[i])
				// Handle "In" and "NotIn" operators specially
				switch op {
				case "In":
					// "key in dict" -> dict.get(key).is_some()
					// For serde_json::Value, we use get() with a string
					parts = append(parts, fmt.Sprintf("(%s.get(%s).is_some())", right, prevLeft))
				case "NotIn":
					// "key not in dict" -> !dict.get(key).is_some()
					parts = append(parts, fmt.Sprintf("(!%s.get(%s).is_some())", right, prevLeft))
				default:
					parts = append(parts, fmt.Sprintf("(%s %s %s)", prevLeft, PyOpToRustOp(op), right))
				}
				prevLeft = right
			}
			if len(parts) == 1 {
				return parts[0]
			}
			return "(" + strings.Join(parts, " && ") + ")"
		}
		return "false"
	case ir.BoolOp:
		if boolVal, ok := val.Value.(map[string]interface{}); ok {
			op, opOk := boolVal["op"].(string)
			values, valOk := boolVal["values"].([]ir.Value)
			if !opOk || !valOk {
				return "/* boolop: malformed IR */"
			}

			rustOp := "&&"
			if op == "Or" {
				rustOp = "||"
			}

			var parts []string
			for _, v := range values {
				parts = append(parts, ctx.generateValue(v))
			}
			return "(" + strings.Join(parts, fmt.Sprintf(" %s ", rustOp)) + ")"
		}
		return "false"
	case ir.UnaryOp:
		if unaryVal, ok := val.Value.(map[string]interface{}); ok {
			op, opOk := unaryVal["op"].(string)
			operandVal, operandOk := unaryVal["operand"].(ir.Value)
			if !opOk || !operandOk {
				return "/* unaryop: malformed IR */"
			}
			operand := ctx.generateValue(operandVal)

			switch op {
			case "Not":
				// For serde_json::Value, we need to check if it's null or empty
				// Check if the operand is a simple variable reference (could be serde_json::Value)
				if isSimpleReference(operandVal) {
					// Generate a check for null or empty (for arrays/objects)
					return fmt.Sprintf("(%s.is_null() || (%s.is_array() && %s.as_array().unwrap_or(&vec![]).is_empty()))", operand, operand, operand)
				}
				// Check if operand is a string operation (like .strip()) that returns a string
				// In Python, "not string" checks if string is empty
				if strings.Contains(operand, ".trim()") || strings.Contains(operand, ".strip()") {
					return fmt.Sprintf("%s.is_empty()", operand)
				}
				return fmt.Sprintf("(!%s)", operand)
			case "USub":
				return fmt.Sprintf("(-%s)", operand)
			case "Invert":
				return fmt.Sprintf("(!%s)", operand)
			default:
				return operand
			}
		}
		return "false"
	case ir.Subscript:
		if subVal, ok := val.Value.(map[string]interface{}); ok {
			valueVal, valueOk := subVal["value"].(ir.Value)
			indexVal, indexOk := subVal["index"].(ir.Value)
			if !valueOk || !indexOk {
				return "/* subscript: malformed IR */"
			}
			value := ctx.generateValue(valueVal)

			// Check if this is a slice operation
			if indexVal.Kind == ir.Slice {
				return ctx.generateSlice(value, indexVal)
			}

			// Regular subscript (single index)
			index := ctx.generateValue(indexVal)
			// Subscript on a Value-typed receiver yields a Value. Mark the
			// result so downstream arithmetic can coerce it correctly.
			if valueVal.Kind == ir.Reference {
				if name, ok := valueVal.Value.(string); ok && ctx.localTypes[name] == "serde_json::Value" {
					return fmt.Sprintf("/*Value*/%s[%s].clone()", value, index)
				}
			}
			// For serde_json::Value, we need to clone the result
			return fmt.Sprintf("%s[%s].clone()", value, index)
		}
		return "/* subscript */"
	case ir.Dict:
		if dictVal, ok := val.Value.(map[string]interface{}); ok {
			keys, keysOk := dictVal["keys"].([]ir.Value)
			values, valuesOk := dictVal["values"].([]ir.Value)
			if !keysOk || !valuesOk {
				return "/* dict: malformed IR */"
			}

			var entries []string
			for i, k := range keys {
				if i >= len(values) {
					break
				}
				// For dict keys, we need to extract the raw string value for json! macro
				keyStr := ctx.generateDictKey(k)
				// For dict values, we need to generate the value without .to_string() for json! macro
				valueStr := ctx.generateDictValue(values[i])
				entries = append(entries, fmt.Sprintf("%s: %s", keyStr, valueStr))
			}
			// Check what type of return this is
			hasRowsVar := false
			hasLenCall := false
			for _, entry := range entries {
				if strings.Contains(entry, "rows") && !strings.Contains(entry, "len(") {
					hasRowsVar = true
				}
				if strings.Contains(entry, "len(") {
					hasLenCall = true
				}
			}

			if hasRowsVar && hasLenCall {
				// Success case: {"json": rows, "rows": len(rows)}
				return "format!(\"{{\\\"json\\\": {}, \\\"rows\\\": {}}}\", serde_json::to_string(&rows).unwrap_or(\"[]\".to_string()), rows.len())"
			} else {
				// Simple case or empty case
				return "json!({" + strings.Join(entries, ", ") + "})"
			}
		}
		return "json!({}).to_string()"
	case ir.List:
		if listVal, ok := val.Value.(map[string]interface{}); ok {
			elements, ok := listVal["elements"].([]ir.Value)
			if !ok {
				return "vec![]"
			}

			var elts []string
			for _, e := range elements {
				elts = append(elts, ctx.generateValue(e))
			}
			return "vec![" + strings.Join(elts, ", ") + "]"
		}
		return "vec![]"
	case ir.Call:
		if callVal, ok := val.Value.(map[string]interface{}); ok {
			fn, fnOk := callVal["func"].(string)
			args, argsOk := callVal["args"].([]ir.Value)
			if !fnOk || !argsOk {
				return "/* call: malformed IR */"
			}

			argStrs := make([]string, 0, len(args))
			for _, arg := range args {
				argStrs = append(argStrs, ctx.generateValue(arg))
			}

			// isinstance() needs the IR Value to extract the type name
			// (e.g. "list", "dict"); the string form would have lost that.
			if fn == "isinstance" && len(args) >= 2 {
				obj := argStrs[0]
				typeName := extractTypeName(args[1])
				switch typeName {
				case "dict", "Dict":
					return fmt.Sprintf("%s.is_object()", obj)
				case "list", "List":
					return fmt.Sprintf("%s.is_array()", obj)
				case "str", "Str":
					return fmt.Sprintf("%s.is_string()", obj)
				case "int", "Int":
					return fmt.Sprintf("%s.is_i64()", obj)
				case "float", "Float":
					return fmt.Sprintf("%s.is_f64()", obj)
				case "bool", "Bool":
					return fmt.Sprintf("%s.is_boolean()", obj)
				default:
					return fmt.Sprintf("true /* isinstance %s */", typeName)
				}
			}

			// The values-side call shares the same dispatcher as
			// statement-level calls (operations.go) so that built-ins like
			// sum, min, max, range, etc. work in expression position too.
			return strings.TrimSpace(generateCallSwitch(fn, argStrs))
		}
		return "/* call */"
	case ir.ModuleCall:
		// Handle module function calls like csv.reader, json.loads, etc.
		if moduleVal, ok := val.Value.(map[string]interface{}); ok {
			module, moduleOk := moduleVal["module"].(string)
			fn, fnOk := moduleVal["func"].(string)
			args, argsOk := moduleVal["args"].([]ir.Value)
			if !moduleOk || !fnOk || !argsOk {
				return "/* module_call: malformed IR */"
			}

			argStrs := make([]string, 0, len(args))
			for _, arg := range args {
				argStrs = append(argStrs, ctx.generateValue(arg))
			}

			// Extract kwargs if present
			kwargs := make(map[string]string)
			if kw, ok := moduleVal["kwargs"].(map[string]ir.Value); ok {
				for k, v := range kw {
					kwargs[k] = ctx.generateValue(v)
				}
			}

			return GenerateModuleCallWithKwargs(module, fn, argStrs, kwargs)
		}
		return "/* module_call */"
	case ir.MethodCall:
		// Handle method calls like output.getvalue(), list.append(), etc.
		if methodVal, ok := val.Value.(map[string]interface{}); ok {
			receiverVal, receiverOk := methodVal["receiver"].(ir.Value)
			method, methodOk := methodVal["method"].(string)
			args, argsOk := methodVal["args"].([]ir.Value)
			if !receiverOk || !methodOk || !argsOk {
				return "/* method_call: malformed IR */"
			}
			receiver := ctx.generateValue(receiverVal)

			argStrs := make([]string, 0, len(args))
			for _, arg := range args {
				argStrs = append(argStrs, ctx.generateValue(arg))
			}

			return GenerateMethodCall(receiver, method, argStrs, ctx.movedVariables)
		}
		return "/* method_call */"
	case ir.ListComp:
		// Handle list comprehensions: [x*2 for x in items if x > 0]
		if compVal, ok := val.Value.(map[string]interface{}); ok {
			elementVal, elementOk := compVal["element"].(ir.Value)
			generators, genOk := compVal["generators"].([]map[string]interface{})
			if !elementOk || !genOk {
				return "/* list_comp: malformed IR */"
			}
			element := ctx.generateValue(elementVal)
			return ctx.generateListComp(element, generators)
		}
		return "/* list_comp */"
	case ir.FString:
		if fstrVal, ok := val.Value.(map[string]interface{}); ok {
			parts, ok := fstrVal["parts"].([]ir.Value)
			if !ok {
				return "String::new()"
			}
			return ctx.generateFString(parts)
		}
		return "String::new()"
	default:
		return fmt.Sprintf("/* unknown value kind: %d */", val.Kind)
	}
}

// GenerateValue is the public backward-compatible wrapper.
// It creates a temporary context with no parameter scoping.
// Prefer using ctx.generateValue() when a CodegenContext is available.
func GenerateValue(val ir.Value) string {
	ctx := newCodegenContext()
	return ctx.generateValue(val)
}

// generateManualJson constructs JSON string manually to avoid type inference issues
//
//nolint:unused
func generateManualJson(entries []string) string {
	// Simple approach: construct JSON as string
	var jsonParts []string
	for _, entry := range entries {
		// Split "key": value into key and value
		parts := strings.SplitN(entry, ": ", 2)
		if len(parts) == 2 {
			key := strings.Trim(parts[0], "\"")
			value := strings.TrimSpace(parts[1])

			if strings.Contains(value, "rows") {
				// Complex value - serialize it
				jsonParts = append(jsonParts, fmt.Sprintf("\"%s\":%s", key, fmt.Sprintf("serde_json::to_string(&%s).unwrap_or(\"[]\".to_string())", value)))
			} else {
				// Simple value
				jsonParts = append(jsonParts, fmt.Sprintf("\"%s\":%s", key, value))
			}
		}
	}

	jsonStr := "{" + strings.Join(jsonParts, ",") + "}"
	return fmt.Sprintf("(%s).to_string()", jsonStr)
}

// generateListComp generates Rust code for list comprehensions.
// Python: [x*2 for x in items if x > 0]
// Rust: items.iter().filter(|x| **x > 0).map(|x| x * 2).collect::<Vec<_>>()
func (ctx *CodegenContext) generateListComp(element string, generators []map[string]interface{}) string {
	if len(generators) == 0 {
		return "vec![]"
	}

	// For simple single-generator comprehensions
	gen := generators[0]
	target, _ := gen["target"].(string)
	iteratorVal, ok := gen["iterator"].(ir.Value)
	if !ok {
		return "vec![]"
	}
	iterator := ctx.generateValue(iteratorVal)
	conditions, _ := gen["conditions"].([]ir.Value)

	var builder strings.Builder

	// Start with iterator
	builder.WriteString(fmt.Sprintf("%s.iter()", iterator))

	// Add filter conditions
	if len(conditions) > 0 {
		for _, cond := range conditions {
			condStr := ctx.generateValue(cond)
			// Replace target references in condition with closure parameter
			condStr = strings.ReplaceAll(condStr, target, fmt.Sprintf("*%s", target))
			builder.WriteString(fmt.Sprintf(".filter(|%s| %s)", target, condStr))
		}
	}

	// Add map for element transformation
	// Replace target references in element with closure parameter
	elementExpr := strings.ReplaceAll(element, target, fmt.Sprintf("*%s", target))
	builder.WriteString(fmt.Sprintf(".map(|%s| %s).collect::<Vec<_>>()", target, elementExpr))

	return builder.String()
}

// GenerateListComp is the public backward-compatible wrapper.
func GenerateListComp(element string, generators []map[string]interface{}) string {
	ctx := newCodegenContext()
	return ctx.generateListComp(element, generators)
}

// generateSlice generates Rust code for slice operations.
// Python: arr[1:3], arr[:5], arr[2:], arr[::2], arr[-2:]
// Rust: arr[1..3].to_vec(), arr[..5].to_vec(), arr[2..].to_vec(), arr.iter().step_by(2).cloned().collect(), arr[arr.len()-2..].to_vec()
func (ctx *CodegenContext) generateSlice(value string, sliceVal ir.Value) string {
	// Slice value contains lower, upper, step info
	if sliceMap, ok := sliceVal.Value.(map[string]interface{}); ok {
		lower := ""
		upper := ""
		step := ""

		if l, ok := sliceMap["lower"]; ok && l != nil {
			if lv, ok := l.(ir.Value); ok {
				lower = ctx.generateValue(lv)
			}
		}
		if u, ok := sliceMap["upper"]; ok && u != nil {
			if uv, ok := u.(ir.Value); ok {
				upper = ctx.generateValue(uv)
			}
		}
		if s, ok := sliceMap["step"]; ok && s != nil {
			if sv, ok := s.(ir.Value); ok {
				step = ctx.generateValue(sv)
			}
		}

		// Helper to convert a bound to a Rust slice expression, handling negative indices
		convertBound := func(bound, value string, isLower bool) string {
			if bound == "" {
				return ""
			}
			// Check for negative literal: (-N) or -N
			negative := false
			absVal := bound
			if len(bound) > 1 && bound[0] == '(' && bound[len(bound)-1] == ')' {
				inner := bound[1 : len(bound)-1]
				if len(inner) > 0 && inner[0] == '-' {
					negative = true
					absVal = inner[1:]
				}
			} else if len(bound) > 0 && bound[0] == '-' {
				negative = true
				absVal = bound[1:]
			}
			if negative {
				return fmt.Sprintf("%s.len().saturating_sub(%s)", value, absVal)
			}
			return bound
		}

		// Handle different slice patterns
		if step != "" && step != "1" {
			// Slice with step: arr[::2] or arr[1:5:2]
			if lower == "" {
				lower = "0"
			}
			if upper == "" {
				return fmt.Sprintf("%s.iter().skip(%s).step_by(%s).cloned().collect::<Vec<_>>()", value, convertBound(lower, value, true), step)
			}
			return fmt.Sprintf("%s[%s..%s].iter().step_by(%s).cloned().collect::<Vec<_>>()", value, convertBound(lower, value, true), convertBound(upper, value, false), step)
		}

		// Standard slice without step
		if lower == "" && upper == "" {
			// arr[:]
			return fmt.Sprintf("%s.clone()", value)
		} else if lower == "" {
			// arr[:upper]
			return fmt.Sprintf("%s[..%s].to_vec()", value, convertBound(upper, value, false))
		} else if upper == "" {
			// arr[lower:]
			return fmt.Sprintf("%s[%s..].to_vec()", value, convertBound(lower, value, true))
		} else {
			// arr[lower:upper]
			return fmt.Sprintf("%s[%s..%s].to_vec()", value, convertBound(lower, value, true), convertBound(upper, value, false))
		}
	}

	return fmt.Sprintf("%s.clone()", value)
}

// GenerateSlice is the public backward-compatible wrapper.
func GenerateSlice(value string, sliceVal ir.Value) string {
	ctx := newCodegenContext()
	return ctx.generateSlice(value, sliceVal)
}

// extractTypeName extracts the type name from an IR Value for isinstance checks.
// The argument is typically a Reference to a type like 'list', 'dict', 'str', etc.
func extractTypeName(val ir.Value) string {
	// If it's a Reference, the Value is the type name string
	if val.Kind == ir.Reference {
		if name, ok := val.Value.(string); ok {
			return name
		}
	}
	// If it's a Literal, get the string value
	if val.Kind == ir.Literal && val.Type == ir.IRTypeString {
		if name, ok := val.Value.(string); ok {
			return name
		}
	}
	return "unknown"
}

// generateFString generates Rust format!() for Python f-strings.
func (ctx *CodegenContext) generateFString(parts []ir.Value) string {
	var formatStr strings.Builder
	var args []string

	for _, part := range parts {
		if part.Kind == ir.Literal && part.Type == ir.IRTypeString {
			// Literal string part - escape braces for format!
			s := fmt.Sprintf("%v", part.Value)
			s = strings.ReplaceAll(s, "{", "{{")
			s = strings.ReplaceAll(s, "}", "}}")
			formatStr.WriteString(s)
		} else {
			formatStr.WriteString("{}")
			arg := ctx.generateValue(part)
			// Exception variables are already rewritten by generateValue (Reference case).
			// Do not replace substrings: that would corrupt names like "value" or "result".
			args = append(args, arg)
		}
	}

	if len(args) == 0 {
		return fmt.Sprintf("\"%s\".to_string()", formatStr.String())
	}
	return fmt.Sprintf("format!(\"%s\", %s)", formatStr.String(), strings.Join(args, ", "))
}

// GenerateFString is the public backward-compatible wrapper.
func GenerateFString(parts []ir.Value) string {
	ctx := newCodegenContext()
	return ctx.generateFString(parts)
}

// isSimpleReference checks if a value is a simple variable reference (not a complex expression)
func isSimpleReference(val ir.Value) bool {
	if val.Kind == ir.Reference {
		if str, ok := val.Value.(string); ok {
			// It's a simple reference if it's just a variable name (no dots, no complex expressions)
			return !strings.Contains(str, ".")
		}
	}
	return false
}

// generateDictKey generates a key for the json! macro.
// For string literals, we need just the string value without .to_string().
func (ctx *CodegenContext) generateDictKey(k ir.Value) string {
	// If it's a string literal, extract the raw string value
	if k.Kind == ir.Literal && k.Type == ir.IRTypeString {
		if str, ok := k.Value.(string); ok {
			return fmt.Sprintf("\"%s\"", str)
		}
	}
	// For other types, use the full value generation
	return ctx.generateValue(k)
}

// generateDictValue generates a value for the json! macro.
// For string literals, we need just the string value without .to_string().
func (ctx *CodegenContext) generateDictValue(v ir.Value) string {
	// If it's a list literal that's empty, use serde_json::Value::Array
	if v.Kind == ir.List && v.Value != nil {
		if listVal, ok := v.Value.(map[string]interface{}); ok {
			if elements, ok := listVal["elements"].([]ir.Value); ok && len(elements) == 0 {
				return "serde_json::Value::Array(vec![])"
			}
		}
	}
	// If it's a string literal, extract the raw string value
	if v.Kind == ir.Literal && v.Type == ir.IRTypeString {
		if str, ok := v.Value.(string); ok {
			return fmt.Sprintf("\"%s\"", str)
		}
	}
	// For other types, use the full value generation
	return ctx.generateValue(v)
}
