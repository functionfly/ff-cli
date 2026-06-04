package restrictions

import (
	"fmt"
	"strings"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// ErrorType represents the type of restriction error
type ErrorType string

const (
	ForbiddenImport    ErrorType = "FORBIDDEN_IMPORT"
	ForbiddenBuiltin   ErrorType = "FORBIDDEN_BUILTIN"
	ForbiddenFeature   ErrorType = "FORBIDDEN_FEATURE"
	ForbiddenPattern   ErrorType = "FORBIDDEN_PATTERN"
	DisallowedType     ErrorType = "DISALLOWED_TYPE"
	UnsupportedFeature ErrorType = "UNSUPPORTED_FEATURE"
)

// CompileError represents a compilation error
type CompileError struct {
	Type       ErrorType
	Message    string
	Line       int
	Suggestion string // Helpful suggestion for how to fix the error
	DocLink    string // Link to documentation
}

func (e CompileError) Error() string {
	msg := fmt.Sprintf("[%s] Line %d: %s", e.Type, e.Line, e.Message)
	if e.Suggestion != "" {
		msg += fmt.Sprintf("\n  Suggestion: %s", e.Suggestion)
	}
	if e.DocLink != "" {
		msg += fmt.Sprintf("\n  See: %s", e.DocLink)
	}
	return msg
}

// FormatError returns a user-friendly error message
func (e CompileError) FormatError() string {
	var builder strings.Builder

	// Header with error type
	builder.WriteString(fmt.Sprintf("❌ Compilation Error: %s\n", e.Type))
	builder.WriteString(fmt.Sprintf("   Line %d: %s\n", e.Line, e.Message))

	// Add suggestion if available
	if e.Suggestion != "" {
		builder.WriteString(fmt.Sprintf("   💡 Tip: %s\n", e.Suggestion))
	}

	// Add doc link if available
	if e.DocLink != "" {
		builder.WriteString(fmt.Sprintf("   📖 Docs: %s\n", e.DocLink))
	}

	return builder.String()
}

// ExecutionMode defines the compilation mode
type ExecutionMode string

const (
	ModeDeterministic ExecutionMode = "deterministic"
	ModeComplex       ExecutionMode = "complex"
	ModeCompatible    ExecutionMode = "compatible"
)

// DeterministicModules is the minimal whitelist for deterministic mode
var DeterministicModules = map[string]bool{
	// Standard library - safe subset only
	"json":        true,
	"math":        true,
	"typing":      true,
	"collections": true,
}

// ComplexModules is the extended whitelist for complex mode
var ComplexModules = map[string]bool{
	// Include all deterministic modules
	"json":        true,
	"math":        true,
	"typing":      true,
	"collections": true,

	// Additional modules for complex mode - deterministic-friendly
	"csv":       true, // CSV parsing/writing (string-based)
	"io":        true, // StringIO/BytesIO only (no file I/O)
	"re":        true, // Regular expressions
	"datetime":  true, // Date/time operations
	"itertools": true, // Iterator utilities
	"functools": true, // Functional programming utilities
	"operator":  true, // Operator functions
	"string":    true, // String constants and utilities
	"textwrap":  true, // Text wrapping/filling
	"hashlib":   true, // Deterministic hashing
	"base64":    true, // Base64 encoding/decoding
	"uuid":      true, // UUID5 only (deterministic)
	"decimal":   true, // Decimal arithmetic
	"fractions": true, // Rational numbers
}

// AllowedModules is the default whitelist (for backward compatibility)
// Deprecated: Use GetAllowedModules(mode) instead
var AllowedModules = DeterministicModules

// ForbiddenBuiltins is the blacklist of forbidden builtin functions
var ForbiddenBuiltins = map[string]bool{
	// I/O operations
	"print": true,
	"input": true,
	"open":  true,

	// Dynamic code execution
	"eval":    true,
	"exec":    true,
	"compile": true,

	// System operations
	"exit":   true,
	"quit":   true,
	"system": true,

	// Random (non-deterministic)
	"random":    true,
	"randint":   true,
	"randrange": true,
	"choice":    true,
	"shuffle":   true,

	// Time (can be non-deterministic)
	"time":  true,
	"sleep": true,

	// Environment
	"getenv": true,
	"setenv": true,

	// Reflection
	"vars":    true,
	"dir":     true,
	"globals": true,
	"locals":  true,

	// File operations
	"remove": true,
	"rename": true,
	"mkdir":  true,
	"rmdir":  true,
	"chdir":  true,

	// Threading
	"threading":       true,
	"multiprocessing": true,

	// Network
	"socket":   true,
	"urllib":   true,
	"requests": true,
}

// ForbiddenPatterns is the blacklist of forbidden code patterns
var ForbiddenPatterns = []string{
	"__import__",
	"getattr",
	"setattr",
	"delattr",
	"hasattr",
}

// AllowedIOOperations is the whitelist for io module (complex mode only)
var AllowedIOOperations = map[string]bool{
	"StringIO":          true,
	"BytesIO":           true,
	"StringIO.write":    true,
	"StringIO.read":     true,
	"StringIO.getvalue": true,
	"StringIO.seek":     true,
	"StringIO.tell":     true,
	"BytesIO.write":     true,
	"BytesIO.read":      true,
	"BytesIO.getvalue":  true,
	"BytesIO.seek":      true,
	"BytesIO.tell":      true,
}

// ForbiddenIOOperations - no file I/O even in complex mode
var ForbiddenIOOperations = map[string]bool{
	"open":           true,
	"FileIO":         true,
	"BufferedReader": true,
	"BufferedWriter": true,
	"BufferedRWPair": true,
	"BufferedRandom": true,
}

// GetAllowedModules returns the allowed modules for a given execution mode
func GetAllowedModules(mode ExecutionMode) map[string]bool {
	switch mode {
	case ModeDeterministic:
		return DeterministicModules
	case ModeComplex:
		return ComplexModules
	case ModeCompatible:
		// In compatible mode, allow all modules (will use MicroPython fallback)
		return nil // nil means no restrictions
	default:
		return DeterministicModules
	}
}

// Enforce checks the Python AST for restriction violations.
// It delegates to EnforceWithMode with ModeDeterministic.
func Enforce(ast *parser.PythonAST) []CompileError {
	return EnforceWithMode(ast, ModeDeterministic)
}

// EnforceWithMode checks the Python AST for restriction violations based on execution mode
func EnforceWithMode(ast *parser.PythonAST, mode ExecutionMode) []CompileError {
	errors := make([]CompileError, 0)
	allowedModules := GetAllowedModules(mode)

	// Check each statement in the module
	for _, stmt := range ast.Module.Body {
		stmtMap, ok := stmt.(map[string]interface{})
		if !ok {
			continue
		}

		// Check function definitions
		if parser.IsFunctionDef(stmtMap) {
			checkFunctionDefWithMode(stmtMap, &errors, mode)
		}

		// Check imports
		if isImport(stmtMap) {
			checkImportWithMode(stmtMap, &errors, allowedModules, mode)
		}
	}

	return errors
}

func checkFunctionDefWithMode(fn map[string]interface{}, errors *[]CompileError, mode ExecutionMode) {
	body := parser.GetFunctionBody(fn)

	for _, stmt := range body {
		stmtMap, ok := stmt.(map[string]interface{})
		if !ok {
			continue
		}

		// Check statements in function body
		checkStatementWithMode(stmtMap, errors, mode)
	}
}

func checkStatementWithMode(stmt map[string]interface{}, errors *[]CompileError, mode ExecutionMode) {
	// Check for forbidden builtins in calls
	if parser.IsCall(stmt) {
		funcName := parser.GetCallFunc(stmt)
		if ForbiddenBuiltins[funcName] {
			suggestion := getSuggestionForBuiltin(funcName)
			*errors = append(*errors, CompileError{
				Type:       ForbiddenBuiltin,
				Message:    fmt.Sprintf("builtin function '%s' is not allowed in %s mode", funcName, mode),
				Line:       0,
				Suggestion: suggestion,
				DocLink:    "https://docs.functionfly.dev/restrictions/builtins",
			})
		}
	}

	// Check for expression statements
	if parser.IsExpr(stmt) {
		if value, ok := stmt["value"].(map[string]interface{}); ok {
			checkExpressionWithMode(value, errors, mode)
		}
	}

	// Check for assignments
	if parser.IsAssign(stmt) {
		value := parser.GetAssignValue(stmt)
		if valueMap, ok := value.(map[string]interface{}); ok {
			checkExpressionWithMode(valueMap, errors, mode)
		}
	}

	// Check for return statements
	if parser.IsReturn(stmt) {
		value := parser.GetReturnValue(stmt)
		if value != nil {
			if valueMap, ok := value.(map[string]interface{}); ok {
				checkExpressionWithMode(valueMap, errors, mode)
			}
		}
	}

	// Check for if statements
	if parser.IsIf(stmt) {
		test := parser.GetIfTest(stmt)
		if testMap, ok := test.(map[string]interface{}); ok {
			checkExpressionWithMode(testMap, errors, mode)
		}

		// Check if body
		for _, ifStmt := range parser.GetIfBody(stmt) {
			if ifStmtMap, ok := ifStmt.(map[string]interface{}); ok {
				checkStatementWithMode(ifStmtMap, errors, mode)
			}
		}

		// Check else body
		for _, elseStmt := range parser.GetIfOrelse(stmt) {
			if elseStmtMap, ok := elseStmt.(map[string]interface{}); ok {
				checkStatementWithMode(elseStmtMap, errors, mode)
			}
		}
	}

	// Check for for loops - allowed in ALL modes now
	if parser.IsFor(stmt) {
		// Recursively check the for loop body
		for _, forStmt := range parser.GetForBody(stmt) {
			if forStmtMap, ok := forStmt.(map[string]interface{}); ok {
				checkStatementWithMode(forStmtMap, errors, mode)
			}
		}
		// Check the iterable expression
		if iter := parser.GetForIter(stmt); iter != nil {
			if iterMap, ok := iter.(map[string]interface{}); ok {
				checkExpressionWithMode(iterMap, errors, mode)
			}
		}
	}

	// Check for while loops - allowed in ALL modes now
	if parser.IsWhile(stmt) {
		// Recursively check the while loop body
		for _, whileStmt := range parser.GetWhileBody(stmt) {
			if whileStmtMap, ok := whileStmt.(map[string]interface{}); ok {
				checkStatementWithMode(whileStmtMap, errors, mode)
			}
		}
		// Check the while condition
		if test := parser.GetWhileTest(stmt); test != nil {
			if testMap, ok := test.(map[string]interface{}); ok {
				checkExpressionWithMode(testMap, errors, mode)
			}
		}
	}

	// Check for try/except blocks - allowed in ALL modes now
	if parser.IsTry(stmt) {
		// Check try body
		for _, tryStmt := range parser.GetTryBody(stmt) {
			if tryStmtMap, ok := tryStmt.(map[string]interface{}); ok {
				checkStatementWithMode(tryStmtMap, errors, mode)
			}
		}
		// Check exception handlers
		for _, handler := range parser.GetTryHandlers(stmt) {
			if handlerMap, ok := handler.(map[string]interface{}); ok {
				for _, handlerStmt := range parser.GetExceptHandlerBody(handlerMap) {
					if handlerStmtMap, ok := handlerStmt.(map[string]interface{}); ok {
						checkStatementWithMode(handlerStmtMap, errors, mode)
					}
				}
			}
		}
		// Check else body (executed if no exception)
		for _, elseStmt := range parser.GetTryOrelse(stmt) {
			if elseStmtMap, ok := elseStmt.(map[string]interface{}); ok {
				checkStatementWithMode(elseStmtMap, errors, mode)
			}
		}
		// Check finally body
		for _, finallyStmt := range parser.GetTryFinalbody(stmt) {
			if finallyStmtMap, ok := finallyStmt.(map[string]interface{}); ok {
				checkStatementWithMode(finallyStmtMap, errors, mode)
			}
		}
	}
}

// checkExpression checks an expression for forbidden constructs (used by internal tests)
//
//nolint:unused
func checkExpression(expr map[string]interface{}, errors *[]CompileError) {
	checkExpressionWithMode(expr, errors, ModeDeterministic)
}

func checkExpressionWithMode(expr map[string]interface{}, errors *[]CompileError, mode ExecutionMode) {
	// In compatible mode, skip most restrictions
	if mode == ModeCompatible {
		return
	}

	// Check for function calls
	if parser.IsCall(expr) {
		funcName := parser.GetCallFunc(expr)
		if ForbiddenBuiltins[funcName] { // In deterministic/compatible mode, all forbidden builtins are blocked
			*errors = append(*errors, CompileError{
				Type:    ForbiddenBuiltin,
				Message: fmt.Sprintf("builtin function '%s' is not allowed in %s mode", funcName, mode),
				Line:    0,
			})
		}
	}

	// Check for attribute access that might be forbidden
	if parser.IsAttribute(expr) {
		attr := parser.GetAttributeAttr(expr)
		if attr == "random" || attr == "time" || attr == "os" || attr == "sys" {
			*errors = append(*errors, CompileError{
				Type:    ForbiddenFeature,
				Message: fmt.Sprintf("access to '%s' is not allowed in deterministic mode", attr),
				Line:    0,
			})
		}
	}

	// Check for subscripts (dict/list access) - these are fine
	// But we should check the value being subscripted
	if parser.IsSubscript(expr) {
		value := parser.GetSubscriptValue(expr)
		if valueMap, ok := value.(map[string]interface{}); ok {
			checkExpressionWithMode(valueMap, errors, mode)
		}
	}

	// Check for list comprehensions - allowed in ALL modes now
	if parser.IsListComp(expr) {
		// Check the element expression
		if elt := parser.GetListCompElt(expr); elt != nil {
			if eltMap, ok := elt.(map[string]interface{}); ok {
				checkExpressionWithMode(eltMap, errors, mode)
			}
		}
		// Check the generators (iter expressions)
		for _, gen := range parser.GetListCompGenerators(expr) {
			// Check the iter expression
			if iter := gen["iter"]; iter != nil {
				if iterMap, ok := iter.(map[string]interface{}); ok {
					checkExpressionWithMode(iterMap, errors, mode)
				}
			}
			// Check the if condition (filter)
			if ifs := gen["ifs"]; ifs != nil {
				if ifsList, ok := ifs.([]interface{}); ok {
					for _, ifCond := range ifsList {
						if ifCondMap, ok := ifCond.(map[string]interface{}); ok {
							checkExpressionWithMode(ifCondMap, errors, mode)
						}
					}
				}
			}
		}
	}
}

func isImport(stmt map[string]interface{}) bool {
	nodeType := parser.GetNodeType(stmt)
	return nodeType == "Import" || nodeType == "ImportFrom"
}

func checkImportWithMode(stmt map[string]interface{}, errors *[]CompileError, allowedModules map[string]bool, mode ExecutionMode) {
	nodeType := parser.GetNodeType(stmt)

	// In compatible mode, all imports are allowed
	if mode == ModeCompatible {
		return
	}

	if nodeType == "Import" {
		// Check Import statements: from stmt.names, check each alias.name
		if names, ok := stmt["names"].([]interface{}); ok {
			for _, nameInterface := range names {
				if nameMap, ok := nameInterface.(map[string]interface{}); ok {
					if name, ok := nameMap["name"].(string); ok {
						if allowedModules != nil && !allowedModules[name] {
							*errors = append(*errors, CompileError{
								Type:    ForbiddenImport,
								Message: fmt.Sprintf("import of module '%s' is not allowed in %s mode", name, mode),
								Line:    0,
							})
						}
					}
				}
			}
		}
	} else if nodeType == "ImportFrom" {
		// Check ImportFrom statements: check stmt.module
		if module, ok := stmt["module"].(string); ok {
			// Check for io module restrictions in complex mode
			if module == "io" && mode == ModeComplex {
				// io module is allowed, but check for forbidden operations later
				return
			}
			if allowedModules != nil && !allowedModules[module] {
				*errors = append(*errors, CompileError{
					Type:    ForbiddenImport,
					Message: fmt.Sprintf("import from module '%s' is not allowed in %s mode", module, mode),
					Line:    0,
				})
			}
		} else if stmt["module"] == nil {
			// Relative imports without module (from . import ...)
			*errors = append(*errors, CompileError{
				Type:    ForbiddenImport,
				Message: "relative imports are not allowed",
				Line:    0,
			})
		}
	}
}

// getSuggestionForBuiltin returns a helpful suggestion for a forbidden builtin
func getSuggestionForBuiltin(funcName string) string {
	suggestions := map[string]string{
		"print":    "Return data as part of your function output instead of printing",
		"input":    "Accept input as function parameters instead of using input()",
		"open":     "Use io.StringIO or io.BytesIO for in-memory operations, or pass file content as input",
		"eval":     "Restructure your code to avoid dynamic code execution",
		"exec":     "Restructure your code to avoid dynamic code execution",
		"compile":  "Restructure your code to avoid dynamic code compilation",
		"exit":     "Return an error value instead of calling exit",
		"quit":     "Return an error value instead of calling quit",
		"random":   "Use deterministic algorithms or accept randomness as input parameter",
		"randint":  "Use deterministic algorithms or accept randomness as input parameter",
		"choice":   "Use deterministic algorithms or accept randomness as input parameter",
		"shuffle":  "Use deterministic algorithms or accept randomness as input parameter",
		"time":     "Use datetime module for deterministic date/time operations",
		"sleep":    "Remove sleep calls - they're not needed in serverless functions",
		"getenv":   "Pass configuration as function parameters instead of using environment variables",
		"setenv":   "Configuration should be passed as function parameters",
		"vars":     "Avoid reflection - use explicit attribute access",
		"dir":      "Avoid reflection - use explicit attribute access",
		"globals":  "Avoid reflection - structure your code with explicit variable references",
		"locals":   "Avoid reflection - structure your code with explicit variable references",
		"remove":   "File operations are not allowed - use in-memory data structures",
		"rename":   "File operations are not allowed - use in-memory data structures",
		"mkdir":    "File operations are not allowed - use in-memory data structures",
		"rmdir":    "File operations are not allowed - use in-memory data structures",
		"chdir":    "File operations are not allowed - use in-memory data structures",
		"socket":   "Network operations are not allowed - make HTTP requests through the platform",
		"urllib":   "Network operations are not allowed - make HTTP requests through the platform",
		"requests": "Network operations are not allowed - make HTTP requests through the platform",
	}

	if suggestion, ok := suggestions[funcName]; ok {
		return suggestion
	}
	return "Consider using an alternative approach that doesn't require this function"
}
