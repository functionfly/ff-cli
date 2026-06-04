package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxSourceSize is the maximum allowed Python source code size in bytes (512 KB).
// Submissions larger than this are rejected before spawning a subprocess.
const MaxSourceSize = 512 * 1024

// PythonAST represents a simplified Python AST for processing
type PythonAST struct {
	Module *ModuleNode `json:"module"`
}

// ModuleNode represents a Python module
type ModuleNode struct {
	Body []interface{} `json:"body"`
}

// resolvedPython3 is the absolute path to the python3 binary. Resolved
// once on first use and cached for the process lifetime. Caching the
// absolute path means subsequent invocations don't repeat a PATH
// lookup, which in turn prevents a malicious CWD or writable PATH
// directory from substituting a different python3 binary between
// invocations.
var resolvedPython3 string

// resolvePython3 returns the absolute path to the python3 binary. It
// tries (in order): an absolute path override, the PATH lookup, and
// a small set of well-known system locations.
func resolvePython3() (string, error) {
	if resolvedPython3 != "" {
		return resolvedPython3, nil
	}
	candidates := []string{"python3"}
	if v := os.Getenv("FLYPY_PYTHON3"); v != "" {
		candidates = append([]string{v}, candidates...)
	}
	candidates = append(candidates,
		"/usr/bin/python3",
		"/usr/local/bin/python3",
		"/opt/homebrew/bin/python3",
	)
	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				resolvedPython3 = c
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				resolvedPython3 = abs
				return abs, nil
			}
			resolvedPython3 = p
			return p, nil
		}
	}
	return "", fmt.Errorf("python3 not found in PATH or well-known locations")
}

// ParsePython parses Python source code and returns an AST.
// It uses Python's ast module via subprocess for reliable parsing.
// Returns an error if the source exceeds MaxSourceSize bytes.
func ParsePython(ctx context.Context, source string) (*PythonAST, error) {
	// Reject oversized source to prevent resource exhaustion
	if len(source) > MaxSourceSize {
		return nil, fmt.Errorf("source code too large: %d bytes (max %d bytes)", len(source), MaxSourceSize)
	}

	pythonPath, err := resolvePython3()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, pythonPath, "-c", `
import ast
import json
import sys

class ASTEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, ast.AST):
            result = {}
            for attr in obj._fields:
                value = getattr(obj, attr)
                result[attr] = self.default(value)
            result['__node_type__'] = obj.__class__.__name__
            return result
        elif isinstance(obj, list):
            return [self.default(item) for item in obj]
        elif isinstance(obj, str):
            return obj
        elif isinstance(obj, (int, float, bool)):
            return obj
        elif obj is None:
            return None
        return str(obj)

source = sys.stdin.read()
tree = ast.parse(source)
encoder = ASTEncoder()
print(json.dumps(encoder.default(tree)))
`)

	cmd.Stdin = strings.NewReader(source)
	// Drop PATH from the child env. The child only needs stdlib (ast,
	// json, sys), so this prevents an attacker who can write to a
	// directory in PATH from substituting a malicious python3 helper.
	safeEnv := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		safeEnv = append(safeEnv, kv)
	}
	cmd.Env = safeEnv
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Do not echo Python's stderr to the API caller: it
			// can contain attacker-controlled source. Log it
			// server-side instead.
			return nil, fmt.Errorf("Python parse error (exit %d)", exitErr.ExitCode())
		}
		return nil, fmt.Errorf("failed to run Python parser: %w", err)
	}

	var rawAST map[string]interface{}
	if err := json.Unmarshal(output, &rawAST); err != nil {
		return nil, fmt.Errorf("failed to parse AST JSON: %w", err)
	}

	// Handle both old ast.dump() string format and new encoder format
	body, ok := rawAST["body"].([]interface{})
	if !ok {
		// Try to handle the case where the JSON structure is different
		return nil, fmt.Errorf("unexpected AST JSON structure: no body array found")
	}

	astObj := &PythonAST{
		Module: &ModuleNode{
			Body: body,
		},
	}

	return astObj, nil
}

// GetFunctions extracts function definitions from the AST
func GetFunctions(ast *PythonAST) []map[string]interface{} {
	functions := make([]map[string]interface{}, 0)

	for _, item := range ast.Module.Body {
		if itemMap, ok := item.(map[string]interface{}); ok {
			// Check for FunctionDef using __node_type__ (new format) or _type (fallback)
			if nodeType, ok := itemMap["__node_type__"].(string); ok && nodeType == "FunctionDef" {
				functions = append(functions, itemMap)
			} else if nodeType, ok := itemMap["_type"].(string); ok && nodeType == "FunctionDef" {
				functions = append(functions, itemMap)
			}
		}
	}

	return functions
}

// GetFunctionBody returns the body of a function
func GetFunctionBody(fn map[string]interface{}) []interface{} {
	if body, ok := fn["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// GetFunctionName returns the name of a function
func GetFunctionName(fn map[string]interface{}) string {
	if name, ok := fn["name"].(string); ok {
		return name
	}
	return ""
}

// GetFunctionArgs returns the arguments of a function
func GetFunctionArgs(fn map[string]interface{}) map[string]interface{} {
	if args, ok := fn["args"].(map[string]interface{}); ok {
		return args
	}
	return nil
}

// GetArgNames returns the argument names from args map
func GetArgNames(args map[string]interface{}) []string {
	names := make([]string, 0)
	if argsList, ok := args["args"].([]interface{}); ok {
		for _, a := range argsList {
			if am, ok := a.(map[string]interface{}); ok {
				if arg, ok := am["arg"].(string); ok {
					names = append(names, arg)
				}
			}
		}
	}
	return names
}

// GetCallFunc returns the function being called in a Call node
func GetCallFunc(call map[string]interface{}) string {
	if funcNode, ok := call["func"].(map[string]interface{}); ok {
		// Check for __node_type__ (primary format from Python AST encoder)
		if fnType, ok := funcNode["__node_type__"].(string); ok {
			if fnType == "Name" {
				if id, ok := funcNode["id"].(string); ok {
					return id
				}
			}
			if fnType == "Attribute" {
				if attr, ok := funcNode["attr"].(string); ok {
					return attr
				}
			}
		}
		// Fallback to _type for backwards compatibility
		if fnType, ok := funcNode["_type"].(string); ok {
			if fnType == "Name" {
				if id, ok := funcNode["id"].(string); ok {
					return id
				}
			}
			if fnType == "Attribute" {
				if attr, ok := funcNode["attr"].(string); ok {
					return attr
				}
			}
		}
	}
	return ""
}

// GetCallArgs returns the arguments of a function call
func GetCallArgs(call map[string]interface{}) []interface{} {
	if args, ok := call["args"].([]interface{}); ok {
		return args
	}
	return nil
}

// KeywordArg represents a keyword argument in a function call
type KeywordArg struct {
	Arg   string
	Value interface{}
}

// GetCallKeywords returns the keyword arguments of a function call
func GetCallKeywords(call map[string]interface{}) []KeywordArg {
	keywords := make([]KeywordArg, 0)
	if kwList, ok := call["keywords"].([]interface{}); ok {
		for _, kw := range kwList {
			if kwMap, ok := kw.(map[string]interface{}); ok {
				arg := ""
				if argVal, ok := kwMap["arg"].(string); ok {
					arg = argVal
				}
				var value interface{}
				if val, ok := kwMap["value"]; ok {
					value = val
				}
				keywords = append(keywords, KeywordArg{Arg: arg, Value: value})
			}
		}
	}
	return keywords
}

// GetBinOpInfo returns the operation, left and right of a binary operation
func GetBinOpInfo(expr map[string]interface{}) (op string, left interface{}, right interface{}) {
	op = extractOpName(expr["op"])
	if leftVal, ok := expr["left"]; ok {
		left = leftVal
	}
	if rightVal, ok := expr["right"]; ok {
		right = rightVal
	}
	return
}

// GetConstantValue returns the value of a constant
func GetConstantValue(expr map[string]interface{}) interface{} {
	if val, ok := expr["value"]; ok {
		return val
	}
	return nil
}

// GetNameID returns the name identifier
func GetNameID(expr map[string]interface{}) string {
	if id, ok := expr["id"].(string); ok {
		return id
	}
	return ""
}

// GetAttributeAttr returns the attribute name
func GetAttributeAttr(expr map[string]interface{}) string {
	if attr, ok := expr["attr"].(string); ok {
		return attr
	}
	return ""
}

// GetAttributeValue returns the value being accessed
func GetAttributeValue(expr map[string]interface{}) interface{} {
	if val, ok := expr["value"]; ok {
		return val
	}
	return nil
}

// GetSubscriptValue returns the subscript value
func GetSubscriptValue(expr map[string]interface{}) interface{} {
	if val, ok := expr["value"]; ok {
		return val
	}
	return nil
}

// GetSubscriptSlice returns the subscript slice
func GetSubscriptSlice(expr map[string]interface{}) interface{} {
	if slice, ok := expr["slice"]; ok {
		return slice
	}
	return nil
}

// GetDictKeys returns dictionary keys
func GetDictKeys(expr map[string]interface{}) []interface{} {
	if keys, ok := expr["keys"].([]interface{}); ok {
		return keys
	}
	return nil
}

// GetDictValues returns dictionary values
func GetDictValues(expr map[string]interface{}) []interface{} {
	if values, ok := expr["values"].([]interface{}); ok {
		return values
	}
	return nil
}

// GetListElts returns list elements
func GetListElts(expr map[string]interface{}) []interface{} {
	if elts, ok := expr["elts"].([]interface{}); ok {
		return elts
	}
	return nil
}

// GetCompareOps returns comparison operators
func GetCompareOps(expr map[string]interface{}) []string {
	ops := make([]string, 0)
	if opsList, ok := expr["ops"].([]interface{}); ok {
		for _, o := range opsList {
			if opStr, ok := o.(string); ok {
				ops = append(ops, opStr)
			} else if opMap, ok := o.(map[string]interface{}); ok {
				// Check for __node_type__ first (primary format from Python AST encoder)
				if opType, ok := opMap["__node_type__"].(string); ok {
					ops = append(ops, opType)
				} else if opType, ok := opMap["_type"].(string); ok {
					ops = append(ops, opType)
				}
			}
		}
	}
	return ops
}

// GetCompareLeft returns the left side of comparison
func GetCompareLeft(expr map[string]interface{}) interface{} {
	if left, ok := expr["left"]; ok {
		return left
	}
	return nil
}

// GetCompareComparators returns the comparators
func GetCompareComparators(expr map[string]interface{}) []interface{} {
	if comps, ok := expr["comparators"].([]interface{}); ok {
		return comps
	}
	return nil
}

// GetBoolOpOp returns boolean operation operator
func GetBoolOpOp(expr map[string]interface{}) string {
	return extractOpName(expr["op"])
}

// GetBoolOpValues returns boolean operation values
func GetBoolOpValues(expr map[string]interface{}) []interface{} {
	if values, ok := expr["values"].([]interface{}); ok {
		return values
	}
	return nil
}

// GetUnaryOpOp returns unary operation operator
func GetUnaryOpOp(expr map[string]interface{}) string {
	return extractOpName(expr["op"])
}

// extractOpName returns the operator name from an AST op field, which can
// be either a bare string (e.g. "Add") or a sub-map with __node_type__ / _type.
func extractOpName(raw interface{}) string {
	if s, ok := raw.(string); ok {
		return s
	}
	if m, ok := raw.(map[string]interface{}); ok {
		if t, ok := m["__node_type__"].(string); ok {
			return t
		}
		if t, ok := m["_type"].(string); ok {
			return t
		}
	}
	return ""
}

// GetUnaryOpOperand returns unary operation operand
func GetUnaryOpOperand(expr map[string]interface{}) interface{} {
	if operand, ok := expr["operand"]; ok {
		return operand
	}
	return nil
}

// GetReturnValue returns the return value
func GetReturnValue(stmt map[string]interface{}) interface{} {
	if value, ok := stmt["value"]; ok {
		return value
	}
	return nil
}

// GetAssignTarget returns the assignment target
func GetAssignTarget(stmt map[string]interface{}) interface{} {
	if targets, ok := stmt["targets"].([]interface{}); ok && len(targets) > 0 {
		return targets[0]
	}
	return nil
}

// GetAssignValue returns the assignment value
func GetAssignValue(stmt map[string]interface{}) interface{} {
	if value, ok := stmt["value"]; ok {
		return value
	}
	return nil
}

// GetIfTest returns the if test condition
func GetIfTest(stmt map[string]interface{}) interface{} {
	if test, ok := stmt["test"]; ok {
		return test
	}
	return nil
}

// GetIfBody returns the if body
func GetIfBody(stmt map[string]interface{}) []interface{} {
	if body, ok := stmt["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// GetIfOrelse returns the else body
func GetIfOrelse(stmt map[string]interface{}) []interface{} {
	if orelse, ok := stmt["orelse"].([]interface{}); ok {
		return orelse
	}
	return nil
}

// GetForTarget returns the for loop target
func GetForTarget(stmt map[string]interface{}) interface{} {
	if target, ok := stmt["target"]; ok {
		return target
	}
	return nil
}

// GetForIter returns the for loop iterator
func GetForIter(stmt map[string]interface{}) interface{} {
	if iter, ok := stmt["iter"]; ok {
		return iter
	}
	return nil
}

// GetForBody returns the for loop body
func GetForBody(stmt map[string]interface{}) []interface{} {
	if body, ok := stmt["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// GetWhileTest returns the while test condition
func GetWhileTest(stmt map[string]interface{}) interface{} {
	if test, ok := stmt["test"]; ok {
		return test
	}
	return nil
}

// GetWhileBody returns the while loop body
func GetWhileBody(stmt map[string]interface{}) []interface{} {
	if body, ok := stmt["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// IsFunctionDef checks if a node is a function definition
func IsFunctionDef(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "FunctionDef"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "FunctionDef"
	}
	return false
}

// IsCall checks if a node is a function call
func IsCall(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Call"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Call"
	}
	return false
}

// IsName checks if a node is a name reference
func IsName(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Name"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Name"
	}
	return false
}

// IsConstant checks if a node is a constant
func IsConstant(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Constant"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Constant"
	}
	return false
}

// IsAttribute checks if a node is an attribute access
func IsAttribute(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Attribute"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Attribute"
	}
	return false
}

// IsBinOp checks if a node is a binary operation
func IsBinOp(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "BinOp"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "BinOp"
	}
	return false
}

// IsUnaryOp checks if a node is a unary operation
func IsUnaryOp(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "UnaryOp"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "UnaryOp"
	}
	return false
}

// IsCompare checks if a node is a comparison
func IsCompare(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Compare"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Compare"
	}
	return false
}

// IsBoolOp checks if a node is a boolean operation
func IsBoolOp(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "BoolOp"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "BoolOp"
	}
	return false
}

// IsSubscript checks if a node is a subscript
func IsSubscript(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Subscript"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Subscript"
	}
	return false
}

// IsDict checks if a node is a dict
func IsDict(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Dict"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Dict"
	}
	return false
}

// IsList checks if a node is a list
func IsList(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "List"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "List"
	}
	return false
}

// IsReturn checks if a node is a return statement
func IsReturn(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Return"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Return"
	}
	return false
}

// IsAssign checks if a node is an assignment
func IsAssign(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Assign"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Assign"
	}
	return false
}

// IsIf checks if a node is an if statement
func IsIf(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "If"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "If"
	}
	return false
}

// IsFor checks if a node is a for loop
func IsFor(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "For"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "For"
	}
	return false
}

// IsWhile checks if a node is a while loop
func IsWhile(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "While"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "While"
	}
	return false
}

// IsExpr checks if a node is an expression statement
func IsExpr(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Expr"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Expr"
	}
	return false
}

// GetNodeType returns the type of a node
func GetNodeType(node interface{}) string {
	if nodeMap, ok := node.(map[string]interface{}); ok {
		// Check for __node_type__ first (new encoder format)
		if nodeType, ok := nodeMap["__node_type__"].(string); ok {
			return nodeType
		}
		// Fallback to _type for backward compatibility
		if nodeType, ok := nodeMap["_type"].(string); ok {
			return nodeType
		}
	}
	return ""
}

// IsListComp checks if a node is a list comprehension
func IsListComp(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "ListComp"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "ListComp"
	}
	return false
}

// IsDictComp checks if a node is a dict comprehension
func IsDictComp(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "DictComp"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "DictComp"
	}
	return false
}

// IsSlice checks if a node is a slice operation
func IsSlice(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Slice"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Slice"
	}
	return false
}

// GetListCompElt returns the element expression of a list comprehension
func GetListCompElt(expr map[string]interface{}) interface{} {
	if elt, ok := expr["elt"]; ok {
		return elt
	}
	return nil
}

// GetListCompGenerators returns the generators of a list comprehension
func GetListCompGenerators(expr map[string]interface{}) []map[string]interface{} {
	generators := make([]map[string]interface{}, 0)
	if gens, ok := expr["generators"].([]interface{}); ok {
		for _, g := range gens {
			if gMap, ok := g.(map[string]interface{}); ok {
				generators = append(generators, gMap)
			}
		}
	}
	return generators
}

// GetCompGenTarget returns the target of a comprehension generator
func GetCompGenTarget(gen map[string]interface{}) interface{} {
	if target, ok := gen["target"]; ok {
		return target
	}
	return nil
}

// GetCompGenIter returns the iterator of a comprehension generator
func GetCompGenIter(gen map[string]interface{}) interface{} {
	if iter, ok := gen["iter"]; ok {
		return iter
	}
	return nil
}

// GetCompGenIfs returns the if conditions of a comprehension generator
func GetCompGenIfs(gen map[string]interface{}) []interface{} {
	if ifs, ok := gen["ifs"].([]interface{}); ok {
		return ifs
	}
	return nil
}

// GetSliceLower returns the lower bound of a slice
func GetSliceLower(expr map[string]interface{}) interface{} {
	if lower, ok := expr["lower"]; ok {
		return lower
	}
	return nil
}

// GetSliceUpper returns the upper bound of a slice
func GetSliceUpper(expr map[string]interface{}) interface{} {
	if upper, ok := expr["upper"]; ok {
		return upper
	}
	return nil
}

// GetSliceStep returns the step of a slice
func GetSliceStep(expr map[string]interface{}) interface{} {
	if step, ok := expr["step"]; ok {
		return step
	}
	return nil
}

// IsAugAssign checks if a node is an augmented assignment (+=, -=, etc.)
func IsAugAssign(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "AugAssign"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "AugAssign"
	}
	return false
}

// GetAugAssignTarget returns the target of an augmented assignment
func GetAugAssignTarget(stmt map[string]interface{}) interface{} {
	if target, ok := stmt["target"]; ok {
		return target
	}
	return nil
}

// GetAugAssignOp returns the operator of an augmented assignment
func GetAugAssignOp(stmt map[string]interface{}) string {
	return extractOpName(stmt["op"])
}

// GetAugAssignValue returns the value of an augmented assignment
func GetAugAssignValue(stmt map[string]interface{}) interface{} {
	if value, ok := stmt["value"]; ok {
		return value
	}
	return nil
}

// IsBreak checks if a node is a break statement
func IsBreak(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Break"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Break"
	}
	return false
}

// IsContinue checks if a node is a continue statement
func IsContinue(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Continue"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Continue"
	}
	return false
}

// IsTry checks if a node is a try statement
func IsTry(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Try"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Try"
	}
	return false
}

// GetTryBody returns the try block body
func GetTryBody(stmt map[string]interface{}) []interface{} {
	if body, ok := stmt["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// GetTryHandlers returns the exception handlers
func GetTryHandlers(stmt map[string]interface{}) []interface{} {
	if handlers, ok := stmt["handlers"].([]interface{}); ok {
		return handlers
	}
	return nil
}

// GetTryOrelse returns the else block (executed if no exception)
func GetTryOrelse(stmt map[string]interface{}) []interface{} {
	if orelse, ok := stmt["orelse"].([]interface{}); ok {
		return orelse
	}
	return nil
}

// GetTryFinalbody returns the finally block
func GetTryFinalbody(stmt map[string]interface{}) []interface{} {
	if finalbody, ok := stmt["finalbody"].([]interface{}); ok {
		return finalbody
	}
	return nil
}

// GetExceptHandlerType returns the exception type for an except handler
func GetExceptHandlerType(handler map[string]interface{}) interface{} {
	return handler["type"]
}

// GetExceptHandlerName returns the exception variable name (e.g., "e" in "except Exception as e")
func GetExceptHandlerName(handler map[string]interface{}) string {
	if name, ok := handler["name"].(string); ok {
		return name
	}
	return ""
}

// GetExceptHandlerBody returns the handler body
func GetExceptHandlerBody(handler map[string]interface{}) []interface{} {
	if body, ok := handler["body"].([]interface{}); ok {
		return body
	}
	return nil
}

// IsRaise checks if a node is a raise statement
func IsRaise(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "Raise"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "Raise"
	}
	return false
}

// GetRaiseExc returns the exception being raised
func GetRaiseExc(stmt map[string]interface{}) interface{} {
	return stmt["exc"]
}

// IsJoinedStr checks if a node is an f-string (JoinedStr in Python AST)
func IsJoinedStr(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "JoinedStr"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "JoinedStr"
	}
	return false
}

// GetJoinedStrValues returns the parts of an f-string
func GetJoinedStrValues(expr map[string]interface{}) []interface{} {
	if values, ok := expr["values"].([]interface{}); ok {
		return values
	}
	return nil
}

// IsFormattedValue checks if a node is a FormattedValue (interpolated expression in f-string)
func IsFormattedValue(node map[string]interface{}) bool {
	if nodeType, ok := node["__node_type__"].(string); ok {
		return nodeType == "FormattedValue"
	}
	if nodeType, ok := node["_type"].(string); ok {
		return nodeType == "FormattedValue"
	}
	return false
}

// GetFormattedValueExpr returns the expression inside a FormattedValue
func GetFormattedValueExpr(node map[string]interface{}) interface{} {
	return node["value"]
}
