package backend

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// CodegenContext holds all mutable state for a single code generation pass.
// Using a struct instead of package-level globals eliminates race conditions
// when multiple compilations run concurrently.
type CodegenContext struct {
	// parameterNames is the set of function parameter names for proper scoping
	parameterNames map[string]bool

	// movedVariables tracks variables that have been moved into other variables
	// key: original variable name, value: new variable name to use
	movedVariables map[string]string

	// declaredVariables tracks variables that have already been declared with `let`
	// so subsequent assignments use reassignment instead of new declarations
	declaredVariables map[string]bool

	// hoistedVariableNames tracks variables that have been hoisted (parameters that are reassigned)
	// These should NOT be prefixed with "input." because they have local mutable copies
	hoistedVariableNames map[string]bool

	// localTypes records the Rust type (e.g. "i32", "String", "f64") for
	// local variables so the value generator can emit the right Rust type
	// when the variable is referenced (the IR's reference kind leaves the
	// type as unknown, which would otherwise force an unnecessary coercion).
	localTypes map[string]string

	// reassignedVars tracks local variables that are reassigned after their
	// initial declaration (via plain reassignment, aug_assign, or assign_subscript).
	// These need `let mut` instead of `let` at their declaration site.
	reassignedVars map[string]bool
}

// newCodegenContext creates a fresh CodegenContext for a compilation pass.
func newCodegenContext() *CodegenContext {
	return &CodegenContext{
		parameterNames:       make(map[string]bool),
		movedVariables:       make(map[string]string),
		declaredVariables:    make(map[string]bool),
		hoistedVariableNames: make(map[string]bool),
		localTypes:           make(map[string]string),
		reassignedVars:       make(map[string]bool),
	}
}

// prefixParameter adds "input." prefix to variable names that are function parameters
// but NOT to hoisted variables (which have local mutable copies).
func (ctx *CodegenContext) prefixParameter(name string) string {
	// If this variable has been hoisted (reassigned), use the local mutable copy
	if ctx.hoistedVariableNames[name] {
		return name
	}
	// If it's a function parameter, prefix with input.
	if ctx.parameterNames[name] {
		return "input." + name
	}
	return name
}

// GenerateFunctionBody generates the Rust function body for an IR function.
// It creates a fresh CodegenContext per call, making it safe for concurrent use.
func GenerateFunctionBody(fn *ir.Function) string {
	ctx := newCodegenContext()

	// Initialize parameter names set for proper scoping
	for _, param := range fn.Parameters {
		ctx.parameterNames[param.Name] = true
	}

	// Mark parameters as declared so they use reassignment instead of new declarations
	for _, param := range fn.Parameters {
		ctx.declaredVariables[param.Name] = true
	}

	// Collect all variables that need to be hoisted (parameters that are reassigned)
	// These need to be declared at function level with let mut
	hoistedParams := collectReassignedParams(fn.Body, fn.Parameters)

	// Pre-scan for local variables that are reassigned after initial declaration.
	// These need `let mut` at their first declaration site.
	ctx.reassignedVars = collectAllReassignedVars(fn.Body)

	// Initialize hoisted variable names tracking - these should NOT use input. prefix
	for _, varName := range hoistedParams {
		ctx.hoistedVariableNames[varName] = true
	}

	var body strings.Builder

	// The user-defined handler function (called by the WASI entry point)
	body.WriteString("fn handler_func(input: Input) -> Output {\n")

	// Generate hoisted parameter declarations inside the function
	for _, varName := range hoistedParams {
		body.WriteString(fmt.Sprintf("    let mut %s = input.%s.clone();\n", varName, varName))
	}

	for _, op := range fn.Body {
		body.WriteString(ctx.generateOperationWithIndent(op, 1))
	}

	// Add default return if function doesn't have explicit return
	if !containsReturnInBody(fn.Body) {
		body.WriteString("    serde_json::Value::Null\n")
	}
	body.WriteString("}\n")

	return body.String()
}

// containsReturn checks if an operation or its children contain a return statement
func containsReturn(op ir.Operation) bool {
	if op.Type == "return" {
		return true
	}
	// Check nested operations
	for _, childOp := range op.Body {
		if containsReturn(childOp) {
			return true
		}
	}
	for _, childOp := range op.ElseBody {
		if containsReturn(childOp) {
			return true
		}
	}
	for _, handler := range op.Handlers {
		for _, childOp := range handler.Body {
			if containsReturn(childOp) {
				return true
			}
		}
	}
	for _, childOp := range op.FinallyBody {
		if containsReturn(childOp) {
			return true
		}
	}
	return false
}

// containsReturnInBody checks if the body contains any return statements
func containsReturnInBody(ops []ir.Operation) bool {
	for _, op := range ops {
		if containsReturn(op) {
			return true
		}
	}
	return false
}

// generateOperationWithIndent generates Rust code with proper indentation for nested blocks.
// This is a method on CodegenContext to avoid global state.
func (ctx *CodegenContext) generateOperationWithIndent(op ir.Operation, indent int) string {
	indentStr := strings.Repeat("    ", indent)

	switch op.Type {
	case "assign":
		value := ctx.generateValue(op.Operands[0])

		// Record the Rust type of the local variable so subsequent references
		// (which the IR represents as IRTypeUnknown) can be emitted with the
		// correct type. We always record the type, even for serde_json::Value,
		// because dict/list literals and json!() calls are common Value
		// sources and downstream arithmetic needs to coerce at the use site.
		ctx.localTypes[op.Result] = GoTypeToRust(op.Type_)

		// Check if this variable was already declared - if so, use reassignment
		if ctx.declaredVariables[op.Result] {
			// For reassignments from input.*, check if we should extract strings
			if strings.HasPrefix(value, "input.") && !strings.Contains(value, "(") && shouldExtractString(op.Result) {
				return fmt.Sprintf("%s%s = %s.as_str().unwrap_or(\"\").to_string();\n", indentStr, op.Result, value)
			}
			// Wrap primitive values if assigned to JSON value variables
			if strings.Contains(value, ".to_string()") {
				return fmt.Sprintf("%s%s = serde_json::Value::String(%s);\n", indentStr, op.Result, value)
			}
			if value == "true" {
				return fmt.Sprintf("%s%s = serde_json::Value::Bool(true);\n", indentStr, op.Result)
			}
			if value == "false" {
				return fmt.Sprintf("%s%s = serde_json::Value::Bool(false);\n", indentStr, op.Result)
			}
			if matched, _ := regexp.MatchString(`^\d+(\.\d+)?$`, value); matched {
				if strings.Contains(value, ".") {
					return fmt.Sprintf("%s%s = serde_json::Value::Number(serde_json::Number::from_f64(%s).unwrap_or(serde_json::Number::from(0)));\n", indentStr, op.Result, value)
				} else {
					return fmt.Sprintf("%s%s = serde_json::Value::Number(serde_json::Number::from(%s));\n", indentStr, op.Result, value)
				}
			}
			return fmt.Sprintf("%s%s = %s;\n", indentStr, op.Result, value)
		}

		// Mark this variable as declared
		ctx.declaredVariables[op.Result] = true

		// Check if the value is a CsvDictWriter - if so, make it mutable
		// and track that the StringIO argument has been moved
		if strings.Contains(value, "CsvDictWriter") {
			// Track that the StringIO variable has been moved into this writer
			// Extract the StringIO variable name from the constructor call
			if strings.Contains(value, "CsvDictWriter::new(") {
				// Extract the variable name between CsvDictWriter::new( and )
				start := strings.Index(value, "CsvDictWriter::new(") + len("CsvDictWriter::new(")
				end := strings.Index(value[start:], ")")
				if end > 0 {
					stringIOVar := strings.TrimSpace(value[start : start+end])
					// Track that this variable is now accessed through the writer
					ctx.movedVariables[stringIOVar] = op.Result
				}
			}
			return fmt.Sprintf("%slet mut %s = %s;\n", indentStr, op.Result, value)
		}
		// Check if the value is a collection that will likely be mutated
		if strings.Contains(value, "vec![") || strings.Contains(value, "HashMap::new") || strings.Contains(value, "Vec::new") {
			return fmt.Sprintf("%slet mut %s = %s;\n", indentStr, op.Result, value)
		}
		// If assigning from input.* (serde_json::Value), we need to clone
		if strings.HasPrefix(value, "input.") && !strings.Contains(value, "(") {
			// Check if this variable is expected to be a string
			if shouldExtractString(op.Result) {
				return fmt.Sprintf("%slet mut %s = %s.as_str().unwrap_or(\"\").to_string();\n", indentStr, op.Result, value)
			}
			return fmt.Sprintf("%slet mut %s = %s.clone();\n", indentStr, op.Result, value)
		}
		// If assigning from dict.get() (serde_json::Value), check if we should extract string
		if strings.Contains(value, ".get(") && shouldExtractString(op.Result) {
			return fmt.Sprintf("%slet mut %s = %s.as_str().unwrap_or(\"\").to_string();\n", indentStr, op.Result, value)
		}
		// Check if this variable will be reassigned later — if so, declare as mutable
		if ctx.reassignedVars[op.Result] {
			return fmt.Sprintf("%slet mut %s = %s;\n", indentStr, op.Result, value)
		}
		return fmt.Sprintf("%slet %s = %s;\n", indentStr, op.Result, value)
	case "assign_subscript":
		// arr[index] = value or dict[key] = value
		if len(op.Operands) >= 3 {
			target := ctx.generateValue(op.Operands[0])
			index := ctx.generateValue(op.Operands[1])
			value := ctx.generateValue(op.Operands[2])
			// For JSON objects, use the insert method
			return fmt.Sprintf("%sif let Some(map) = %s.as_object_mut() {\n%s    map.insert(%s.to_string(), %s.into());\n%s}\n", indentStr, target, indentStr, index, value, indentStr)
		}
		return fmt.Sprintf("%s// assign_subscript (missing operands)\n", indentStr)
	case "return":
		if len(op.Operands) > 0 {
			return fmt.Sprintf("%sreturn Output { result: %s };\n", indentStr, ctx.generateValue(op.Operands[0]))
		}
		return fmt.Sprintf("%sreturn Output { result: serde_json::Value::Null };\n", indentStr)
	case "expr":
		// Expression statement (for side effects)
		if op.Value != nil {
			if val, ok := op.Value.(ir.Value); ok {
				return fmt.Sprintf("%s%s;\n", indentStr, ctx.generateValue(val))
			}
		}
		return fmt.Sprintf("%s// expr\n", indentStr)
	case "if":
		return ctx.generateIfStatement(op, indent)
	case "for":
		return ctx.generateForLoop(op, indent)
	case "while":
		return ctx.generateWhileLoop(op, indent)
	case "call":
		return ctx.generateCall(op)
	case "break":
		return fmt.Sprintf("%sbreak;\n", indentStr)
	case "continue":
		return fmt.Sprintf("%scontinue;\n", indentStr)
	case "aug_assign":
		// Augmented assignment: x += 1, x -= 1, etc.
		if len(op.Operands) > 0 {
			rustOp := PyAugOpToRustOp(op.Module) // Module field holds the operator
			return fmt.Sprintf("%s%s %s= %s;\n", indentStr, op.Result, rustOp, ctx.generateValue(op.Operands[0]))
		}
		return fmt.Sprintf("%s// aug_assign\n", indentStr)
	case "try":
		return ctx.generateTryBlock(op, indent)
	case "raise":
		// Raise exception - in Rust we use panic! or Result types
		if len(op.Operands) > 0 && op.Operands[0].Value != nil {
			return fmt.Sprintf("%sreturn Err(format!(\"{:?}\", %s));\n", indentStr, ctx.generateValue(op.Operands[0]))
		}
		return fmt.Sprintf("%sreturn Err(\"exception raised\".to_string());\n", indentStr)
	default:
		return fmt.Sprintf("%s// %s\n", indentStr, op.Type)
	}
}

// generateIfStatement generates Rust if/else statements.
// Handles variable hoisting for variables assigned in both branches.
func (ctx *CodegenContext) generateIfStatement(op ir.Operation, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("    ", indent)

	// Collect variables assigned in both if and else branches that need hoisting
	hoistedVars := collectHoistedVariables(op.Body, op.ElseBody)

	// Check if else block has a return statement - if so, we don't need to initialize
	// the hoisted variables since they're guaranteed to be assigned before use
	elseHasReturn := hasReturnStatement(op.ElseBody)

	// Generate hoisted variable declarations
	for _, varName := range hoistedVars {
		if isLikelyJsonVariable(varName) {
			// JSON variables can be safely initialized as Value::Null
			if elseHasReturn {
				// Don't initialize - the variable is guaranteed to be assigned before use
				result.WriteString(fmt.Sprintf("%slet mut %s;\n", indentStr, varName))
			} else {
				result.WriteString(fmt.Sprintf("%slet mut %s = serde_json::Value::Null;\n", indentStr, varName))
			}
		} else {
			// Concrete type variables - declare without initialization since they're assigned in both branches
			result.WriteString(fmt.Sprintf("%slet mut %s;\n", indentStr, varName))
		}
		// Mark this variable as declared so subsequent assignments use = instead of let
		ctx.declaredVariables[varName] = true
	}

	// Generate if condition
	condStr := ctx.generateValue(op.Condition)
	result.WriteString(fmt.Sprintf("%sif %s {\n", indentStr, condStr))

	// Generate if body (with hoisted variable assignment instead of declaration)
	for _, bodyOp := range op.Body {
		result.WriteString(ctx.generateOperationWithIndentHoisted(bodyOp, indent+1, hoistedVars))
	}

	// Generate else if present
	if op.HasElse && len(op.ElseBody) > 0 {
		result.WriteString(fmt.Sprintf("%s} else {\n", indentStr))
		for _, elseOp := range op.ElseBody {
			result.WriteString(ctx.generateOperationWithIndentHoisted(elseOp, indent+1, hoistedVars))
		}
	}

	result.WriteString(fmt.Sprintf("%s}\n", indentStr))
	return result.String()
}

// collectHoistedVariables finds variables that are assigned in both if and else branches.
// It also recursively handles nested if-elif-else chains.
// Also handles cases where else branch returns early (variable only assigned in if branch).
// Only hoists variables that are likely to be JSON values or need special handling.
func collectHoistedVariables(ifBody, elseBody []ir.Operation) []string {
	ifVars := collectAssignedVars(ifBody)
	elseVars := collectAssignedVars(elseBody)

	// Find intersection - variables assigned in both branches
	hoisted := make([]string, 0)
	for varName := range ifVars {
		if elseVars[varName] {
			// Hoist all variables assigned in both branches - they need to be declared
			// outside the if statement regardless of type
			hoisted = append(hoisted, varName)
		}
	}

	// Also check for variables assigned in if-block where else-block returns early
	// In this case, the variable is always assigned if we reach code after the if-else
	if hasReturnStatement(elseBody) {
		for varName := range ifVars {
			if !contains(hoisted, varName) && isLikelyJsonVariable(varName) {
				hoisted = append(hoisted, varName)
			}
		}
	}

	// Also check for variables assigned in nested if-elif chains
	// If the else body is another if statement (elif), we need to check recursively
	if len(elseBody) == 1 && elseBody[0].Type == "if" {
		nestedIf := elseBody[0]
		// Variables in if branch that are also in nested if/else
		nestedIfVars := collectAssignedVars(nestedIf.Body)
		nestedElseVars := collectAssignedVars(nestedIf.ElseBody)

		// Add variables that appear in both the outer if and nested branches
		for varName := range ifVars {
			if (nestedIfVars[varName] || nestedElseVars[varName]) && isLikelyJsonVariable(varName) {
				if !contains(hoisted, varName) {
					hoisted = append(hoisted, varName)
				}
			}
		}
	}

	// Also check for variables assigned in try blocks within else branch
	// If the else body contains a try statement, check variables assigned in try body
	if len(elseBody) == 1 && elseBody[0].Type == "try" {
		nestedTry := elseBody[0]
		tryVars := collectAssignedVars(nestedTry.Body)

		// Add variables that appear in both the outer if and try body
		for varName := range ifVars {
			if tryVars[varName] && isLikelyJsonVariable(varName) {
				if !contains(hoisted, varName) {
					hoisted = append(hoisted, varName)
				}
			}
		}
	}

	return hoisted
}

// isLikelyJsonVariable checks if a variable name suggests it contains JSON data
// or is used in both branches of conditionals where types may differ (dict.get(),
// different literal types). For these we initialize as serde_json::Value::Null
// to avoid "possibly uninitialized" and type mismatches in complex conditionals.
func isLikelyJsonVariable(varName string) bool {
	// Variables that typically contain JSON values
	jsonLikeNames := []string{
		"event", "data", "result", "output", "response", "parsed",
		"value", "val", "v", "item", "key", "row", "field", "cell",
		"x", "obj", "payload", "body", "record", "entry",
	}
	for _, name := range jsonLikeNames {
		if varName == name {
			return true
		}
	}
	// Variables that come from input
	if strings.HasPrefix(varName, "input_") {
		return true
	}
	return false
}

// hasReturnStatement checks if a block has a return statement
func hasReturnStatement(ops []ir.Operation) bool {
	for _, op := range ops {
		if op.Type == "return" {
			return true
		}
		// Check nested blocks
		if op.Type == "if" {
			if hasReturnStatement(op.Body) || hasReturnStatement(op.ElseBody) {
				return true
			}
		}
	}
	return false
}

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// collectAssignedVars collects all variable names assigned in a block
func collectAssignedVars(ops []ir.Operation) map[string]bool {
	vars := make(map[string]bool)
	for _, op := range ops {
		if op.Type == "assign" && op.Result != "" {
			vars[op.Result] = true
		}
		// Also check nested operations
		if op.Type == "if" {
			for _, nested := range op.Body {
				if nested.Type == "assign" && nested.Result != "" {
					vars[nested.Result] = true
				}
			}
			for _, nested := range op.ElseBody {
				if nested.Type == "assign" && nested.Result != "" {
					vars[nested.Result] = true
				}
			}
		}
		// Also check try blocks
		if op.Type == "try" {
			for _, nested := range op.Body {
				if nested.Type == "assign" && nested.Result != "" {
					vars[nested.Result] = true
				}
			}
			// Also check handler bodies
			for _, handler := range op.Handlers {
				for _, nested := range handler.Body {
					if nested.Type == "assign" && nested.Result != "" {
						vars[nested.Result] = true
					}
				}
			}
		}
	}
	return vars
}

// collectAllReassignedVars scans the IR to find local variables that are
// reassigned after their initial declaration. This includes:
// - Variables assigned more than once (e.g., x = 1; x = 2;)
// - Variables modified via aug_assign (e.g., x += 1)
// - Variables modified via assign_subscript (e.g., d["key"] = value)
// These variables need `let mut` at their first declaration site.
func collectAllReassignedVars(ops []ir.Operation) map[string]bool {
	// First pass: count how many times each variable is assigned
	assignCounts := make(map[string]bool)
	reassigned := make(map[string]bool)

	var scan func(ops []ir.Operation)
	scan = func(ops []ir.Operation) {
		for _, op := range ops {
			switch op.Type {
			case "assign":
				if op.Result != "" {
					if assignCounts[op.Result] {
						// Already assigned before — this is a reassignment
						reassigned[op.Result] = true
					}
					assignCounts[op.Result] = true
				}
			case "aug_assign":
				// x += 1, x -= 1, etc. always requires mut
				if op.Result != "" {
					reassigned[op.Result] = true
				}
			case "assign_subscript":
				// d["key"] = value requires mut on the target
				if len(op.Operands) > 0 {
					if op.Operands[0].Kind == ir.Reference {
						if name, ok := op.Operands[0].Value.(string); ok {
							reassigned[name] = true
						}
					}
				}
			case "if":
				scan(op.Body)
				scan(op.ElseBody)
			case "for", "while":
				scan(op.Body)
			case "try":
				scan(op.Body)
				for _, handler := range op.Handlers {
					scan(handler.Body)
				}
				scan(op.FinallyBody)
			}
		}
	}

	scan(ops)
	return reassigned
}

// collectReassignedParams finds parameters that are reassigned in the function body.
// These need to be hoisted with let mut declarations at function level.
func collectReassignedParams(ops []ir.Operation, params []ir.Parameter) []string {
	// Create a set of parameter names
	paramNames := make(map[string]bool)
	for _, p := range params {
		paramNames[p.Name] = true
	}

	// Collect all assigned variables
	assignedVars := collectAssignedVars(ops)

	// Find parameters that are reassigned
	var result []string
	for varName := range assignedVars {
		if paramNames[varName] {
			result = append(result, varName)
		}
	}

	return result
}

// rhsLooksLikeJsonValue returns true if the Rust expression is already a serde_json::Value
// (e.g. from .get(), input.*, Value::*, json!(), etc.).
func rhsLooksLikeJsonValue(value string) bool {
	return strings.Contains(value, "serde_json::Value") ||
		strings.Contains(value, ".get(") ||
		strings.HasPrefix(value, "input.") ||
		strings.Contains(value, "Value::Null") ||
		strings.Contains(value, "json!")
}

// wrapRHSForJsonVar wraps a non-Value RHS (string/number literal) so it assigns to a Value variable.
// Used for hoisted JSON variables when one branch assigns a Value and another a literal.
func wrapRHSForJsonVar(value string) string {
	if rhsLooksLikeJsonValue(value) {
		return value
	}
	// String literal or any expression that produces a String (e.g. .to_string(), format!())
	if strings.HasPrefix(value, "\"") || strings.Contains(value, ".to_string()") {
		return fmt.Sprintf("serde_json::Value::String(%s)", value)
	}
	// Boolean literals
	if value == "true" {
		return "serde_json::Value::Bool(true)"
	}
	if value == "false" {
		return "serde_json::Value::Bool(false)"
	}
	// Numeric literal (simple, no parens)
	if len(value) > 0 && value[0] >= '0' && value[0] <= '9' && !strings.Contains(value, "(") {
		if strings.Contains(value, ".") {
			return fmt.Sprintf("serde_json::Value::Number(serde_json::Number::from_f64(%s).unwrap_or(serde_json::Number::from(0)))", value)
		}
		return fmt.Sprintf("serde_json::Value::Number(serde_json::Number::from(%s))", value)
	}
	return value
}

// generateOperationWithIndentHoisted generates Rust code with proper indentation,
// converting let bindings to assignments for hoisted variables.
func (ctx *CodegenContext) generateOperationWithIndentHoisted(op ir.Operation, indent int, hoistedVars []string) string {
	indentStr := strings.Repeat("    ", indent)

	// Check if this is an assignment to a hoisted variable
	if op.Type == "assign" && isHoisted(op.Result, hoistedVars) {
		// Generate assignment instead of let binding, but handle JSON values properly
		value := ctx.generateValue(op.Operands[0])
		// For reassignments from input.*, check if we should extract strings
		if strings.HasPrefix(value, "input.") && !strings.Contains(value, "(") && shouldExtractString(op.Result) {
			return fmt.Sprintf("%s%s = %s.as_str().unwrap_or(\"\").to_string();\n", indentStr, op.Result, value)
		}
		// Handle dict.get() calls that should return strings
		if strings.Contains(value, ".get(") && shouldExtractString(op.Result) {
			return fmt.Sprintf("%s%s = %s.as_str().unwrap_or(\"\").to_string();\n", indentStr, op.Result, value)
		}
		// Hoisted JSON vars are initialized as Value::Null; ensure RHS is Value when needed
		if isLikelyJsonVariable(op.Result) && !rhsLooksLikeJsonValue(value) {
			value = wrapRHSForJsonVar(value)
		}
		return fmt.Sprintf("%s%s = %s;\n", indentStr, op.Result, value)
	}

	// For nested if statements, pass hoisted vars down
	if op.Type == "if" {
		return ctx.generateIfStatementHoisted(op, indent, hoistedVars)
	}

	// For other operations, use the standard generator
	return ctx.generateOperationWithIndent(op, indent)
}

// generateIfStatementHoisted generates if statements with hoisted variable tracking.
func (ctx *CodegenContext) generateIfStatementHoisted(op ir.Operation, indent int, parentHoistedVars []string) string {
	var result strings.Builder
	indentStr := strings.Repeat("    ", indent)

	// Generate if condition
	condStr := ctx.generateValue(op.Condition)
	result.WriteString(fmt.Sprintf("%sif %s {\n", indentStr, condStr))

	// Generate if body (with hoisted variable assignment instead of declaration)
	for _, bodyOp := range op.Body {
		result.WriteString(ctx.generateOperationWithIndentHoisted(bodyOp, indent+1, parentHoistedVars))
	}

	// Generate else if present
	if op.HasElse && len(op.ElseBody) > 0 {
		result.WriteString(fmt.Sprintf("%s} else {\n", indentStr))
		for _, elseOp := range op.ElseBody {
			result.WriteString(ctx.generateOperationWithIndentHoisted(elseOp, indent+1, parentHoistedVars))
		}
	}

	result.WriteString(fmt.Sprintf("%s}\n", indentStr))
	return result.String()
}

// isHoisted checks if a variable name is in the hoisted list
func isHoisted(name string, hoistedVars []string) bool {
	for _, v := range hoistedVars {
		if v == name {
			return true
		}
	}
	return false
}

// shouldExtractString checks if a variable name suggests it should contain a string
// rather than a JSON value. This helps with type inference for assignments.
func shouldExtractString(varName string) bool {
	// Common patterns for string variables
	return strings.Contains(varName, "data") ||
		strings.Contains(varName, "text") ||
		strings.Contains(varName, "str") ||
		strings.Contains(varName, "csv") ||
		strings.Contains(varName, "content") ||
		strings.Contains(varName, "input") ||
		strings.Contains(varName, "null_value") ||
		varName == "value" // common temporary variable
}

// generateForLoop generates Rust for loops.
func (ctx *CodegenContext) generateForLoop(op ir.Operation, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("    ", indent)

	// Check for tuple unpacking (for i, row in enumerate(data))
	var targetStr string
	if tupleNames, ok := op.Value.([]string); ok && len(tupleNames) > 0 {
		// Tuple unpacking: for (i, row) in iter
		targetStr = fmt.Sprintf("(%s)", strings.Join(tupleNames, ", "))
	} else if op.Target != "" {
		targetStr = op.Target
	} else {
		targetStr = "_item" // fallback
	}

	// Generate for loop header
	// For serde_json::Value iterators, we need to use as_array().unwrap().iter()
	iterStr := ctx.generateValue(op.Iterator)
	// Check if the iterator is a variable that could be a serde_json::Value
	if isJsonValueVariable(iterStr) {
		iterStr = fmt.Sprintf("%s.as_array().unwrap_or(&vec![]).iter()", iterStr)
	}
	result.WriteString(fmt.Sprintf("%sfor %s in %s {\n", indentStr, targetStr, iterStr))

	// Generate for body
	for _, bodyOp := range op.Body {
		result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent+1))
	}

	result.WriteString(fmt.Sprintf("%s}\n", indentStr))
	return result.String()
}

// generateWhileLoop generates Rust while loops.
func (ctx *CodegenContext) generateWhileLoop(op ir.Operation, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("    ", indent)

	// Generate while loop header
	condStr := ctx.generateValue(op.Condition)
	result.WriteString(fmt.Sprintf("%swhile %s {\n", indentStr, condStr))

	// Generate while body
	for _, bodyOp := range op.Body {
		result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent+1))
	}

	result.WriteString(fmt.Sprintf("%s}\n", indentStr))
	return result.String()
}

// GenerateOperation generates Rust code for a single operation (public wrapper).
func GenerateOperation(op ir.Operation) string {
	ctx := newCodegenContext()
	return ctx.generateOperationWithIndent(op, 1)
}

// GenerateOperationWithIndent is the public wrapper for backward compatibility.
// New code should prefer using a CodegenContext directly.
func GenerateOperationWithIndent(op ir.Operation, indent int) string {
	ctx := newCodegenContext()
	return ctx.generateOperationWithIndent(op, indent)
}

// generateTryBlock generates Rust code for try/except blocks.
// For deterministic code, we convert try/except to Result/Option patterns.
func (ctx *CodegenContext) generateTryBlock(op ir.Operation, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("    ", indent)

	// For deterministic WASM code, generate try body inline
	// Exception handling is done at the operation level with Result/Option
	result.WriteString(fmt.Sprintf("%s// try block (exceptions handled at operation level)\n", indentStr))

	// Generate try body, but handle continue/break specially
	bodyOps := op.Body
	for i, bodyOp := range bodyOps {
		if bodyOp.Type == "continue" && i > 0 {
			// This continue follows some operation - wrap the previous operation in success check
			if i == 1 { // pattern: operation; continue
				prevOp := bodyOps[0]
				if prevOp.Type == "assign" && strings.Contains(ctx.generateValue(prevOp.Operands[0]), "parse::<") {
					// This is a parsing assignment followed by continue
					// Generate as: if let Ok(val) = parse_expr { assign; continue; }
					// But for now, just generate both and accept the unreachable warning
					result.WriteString(ctx.generateOperationWithIndent(prevOp, indent))
					result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent))
				} else {
					result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent))
				}
			} else {
				result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent))
			}
		} else {
			result.WriteString(ctx.generateOperationWithIndent(bodyOp, indent))
		}
	}

	// In deterministic mode, exceptions are handled at operation level with Result/Option
	// So we don't generate except blocks. The try body should handle all error cases.
	// If we reach here, it means the try body completed successfully.

	// Generate finally block
	if len(op.FinallyBody) > 0 {
		result.WriteString(fmt.Sprintf("%s// finally\n", indentStr))
		for _, fOp := range op.FinallyBody {
			result.WriteString(ctx.generateOperationWithIndent(fOp, indent))
		}
	}

	return result.String()
}
