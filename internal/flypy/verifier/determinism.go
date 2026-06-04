package verifier

import (
	"fmt"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// DeterminismError represents a determinism verification error
type DeterminismError struct {
	Function  string
	Operation string
	Message   string
}

func (e DeterminismError) Error() string {
	return fmt.Sprintf("determinism error in %s: %s", e.Function, e.Message)
}

// SideEffectType represents the type of side effect
type SideEffectType string

const (
	SideEffectNone          SideEffectType = "none"
	SideEffectNetwork       SideEffectType = "network"
	SideEffectExternalState SideEffectType = "external_state"
	SideEffectMutation      SideEffectType = "mutation"
	SideEffectIO            SideEffectType = "io"
)

// SideEffect represents a side effect
type SideEffect struct {
	Type      SideEffectType
	Function  string
	Location  string
	Operation string
	Target    string
	Message   string
}

// Verify verifies that the IR module is deterministic
func Verify(module *ir.Module) []DeterminismError {
	errors := make([]DeterminismError, 0)

	for _, fn := range module.Functions {
		errs := verifyFunction(fn)
		errors = append(errors, errs...)
	}

	return errors
}

func verifyFunction(fn *ir.Function) []DeterminismError {
	return verifyOperations(fn.Name, fn.Body)
}

func verifyOperations(funcName string, ops []ir.Operation) []DeterminismError {
	errors := make([]DeterminismError, 0)

	for _, op := range ops {
		if !isDeterministic(op) {
			errors = append(errors, DeterminismError{
				Function:  funcName,
				Operation: op.Type,
				Message:   fmt.Sprintf("operation '%s' is not deterministic", op.Type),
			})
		}
		// Recurse into block bodies
		errors = append(errors, verifyOperations(funcName, op.Body)...)
		errors = append(errors, verifyOperations(funcName, op.ElseBody)...)
		errors = append(errors, verifyOperations(funcName, op.FinallyBody)...)
		for _, handler := range op.Handlers {
			errors = append(errors, verifyOperations(funcName, handler.Body)...)
		}
	}

	return errors
}

func isDeterministic(op ir.Operation) bool {
	deterministicOps := map[string]bool{
		"assign":           true,
		"assign_subscript": true,
		"return":           true,
		"add":              true,
		"sub":              true,
		"mul":              true,
		"div":              true,
		"mod":              true,
		"pow":              true,
		"eq":               true,
		"ne":               true,
		"lt":               true,
		"le":               true,
		"gt":               true,
		"ge":               true,
		"and":              true,
		"or":               true,
		"not":              true,
		"dict_get":         true,
		"dict_set":         true,
		"list_get":         true,
		"list_append":      true,
		"if":               true,
		"for":              true,
		"while":            true,
		"try":              true,
		"break":            true,
		"continue":         true,
		"raise":            true,
		"expr":             true,
		"aug_assign":       true,
	}

	if deterministicOps[op.Type] {
		return true
	}

	if op.Type == "call" {
		if callValue, ok := op.Value.(map[string]interface{}); ok {
			if fn, ok := callValue["func"].(string); ok {
				allowedFuncs := map[string]bool{
					"len":        true,
					"str":        true,
					"int":        true,
					"float":      true,
					"bool":       true,
					"abs":        true,
					"min":        true,
					"max":        true,
					"sum":        true,
					"range":      true,
					"enumerate":  true,
					"zip":        true,
					"map":        true,
					"filter":     true,
					"sorted":     true,
					"reversed":   true,
					"any":        true,
					"all":        true,
					"isinstance": true,
					"list":       true,
					"dict":       true,
					"tuple":      true,
					"set":        true,
					"type":       true,
					"print":      true,
				}
				return allowedFuncs[fn]
			}
		}
	}

	if op.Kind == ir.BinOp {
		return true
	}

	if op.Kind == ir.Compare {
		return true
	}

	if op.Kind == ir.BoolOp {
		return true
	}

	if op.Kind == ir.Subscript {
		return true
	}

	if op.Kind == ir.Dict || op.Kind == ir.List {
		return true
	}

	return false
}

// AnalyzeSideEffects analyzes side effects in the IR module
func AnalyzeSideEffects(module *ir.Module) []SideEffect {
	effects := make([]SideEffect, 0)

	for _, fn := range module.Functions {
		fnEffects := analyzeFunctionSideEffects(fn)
		effects = append(effects, fnEffects...)
	}

	return effects
}

func analyzeFunctionSideEffects(fn *ir.Function) []SideEffect {
	effects := make([]SideEffect, 0)
	funcName := fn.Name

	for _, op := range fn.Body {
		if op.Type == "call" {
			if callValue, ok := op.Value.(map[string]interface{}); ok {
				if calledFn, ok := callValue["func"].(string); ok {
					// These functions have side effects
					sideEffectFuncs := map[string]bool{
						"print":    true,
						"open":     true,
						"requests": true,
						"http":     true,
					}
					if sideEffectFuncs[calledFn] {
						effects = append(effects, SideEffect{
							Type:      SideEffectIO,
							Function:  funcName,
							Location:  funcName,
							Operation: calledFn,
							Message:   fmt.Sprintf("I/O operation: %s()", calledFn),
						})
					}
				}
			}
		}
	}

	return effects
}

// IsPure checks if a function is pure (no side effects, deterministic)
func IsPure(fn *ir.Function) bool {
	effects := analyzeFunctionSideEffects(fn)
	return len(effects) == 0
}
