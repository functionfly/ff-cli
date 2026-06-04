package ir

import (
	"fmt"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// Generate generates an IR module from Python AST
func Generate(pythonAST *parser.PythonAST, name string) (*Module, error) {
	module := &Module{
		Name:      name,
		Functions: make([]*Function, 0),
		Imports:   make([]string, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Extract functions from AST
	functions := parser.GetFunctions(pythonAST)

	for _, fn := range functions {
		irFunc, err := convertFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to convert function %s: %w", parser.GetFunctionName(fn), err)
		}
		module.Functions = append(module.Functions, irFunc)
	}

	return module, nil
}

func convertFunction(fn map[string]interface{}) (*Function, error) {
	funcName := parser.GetFunctionName(fn)
	args := parser.GetFunctionArgs(fn)
	argNames := parser.GetArgNames(args)
	body := parser.GetFunctionBody(fn)

	irFunc := &Function{
		Name:          funcName,
		Parameters:    make([]Parameter, 0),
		Body:          make([]Operation, 0),
		ReturnType:    IRTypeUnknown,
		Pure:          true,
		Deterministic: true,
	}

	// Add parameters
	for _, argName := range argNames {
		irFunc.Parameters = append(irFunc.Parameters, Parameter{
			Name: argName,
			Type: IRType{Base: "unknown", IsInput: true}, // Input parameters are serde_json::Value
		})
	}

	// Convert body statements
	for _, stmt := range body {
		if stmtMap, ok := stmt.(map[string]interface{}); ok {
			ops, err := convertStatement(stmtMap)
			if err != nil {
				return nil, err
			}
			irFunc.Body = append(irFunc.Body, ops...)
		}
	}

	return irFunc, nil
}

func convertStatement(stmt map[string]interface{}) ([]Operation, error) {
	ops := make([]Operation, 0)

	if parser.IsReturn(stmt) {
		// Handle return statement
		retVal := parser.GetReturnValue(stmt)
		if retVal != nil {
			if retMap, ok := retVal.(map[string]interface{}); ok {
				val, _, err := convertExpression(retMap)
				if err != nil {
					return nil, err
				}
				ops = append(ops, Operation{
					Type:     "return",
					Operands: []Value{val},
				})
			}
		} else {
			// Return without value (returns None)
			ops = append(ops, Operation{
				Type:     "return",
				Operands: []Value{{Type: IRTypeNone, Kind: Literal, Value: nil}},
			})
		}
	} else if parser.IsAssign(stmt) {
		// Handle assignment
		target := parser.GetAssignTarget(stmt)
		value := parser.GetAssignValue(stmt)

		if targetMap, ok := target.(map[string]interface{}); ok {
			if parser.IsName(targetMap) {
				varName := parser.GetNameID(targetMap)

				if valueMap, ok := value.(map[string]interface{}); ok {
					val, valType, err := convertExpression(valueMap)
					if err != nil {
						return nil, err
					}
					ops = append(ops, Operation{
						Type:     "assign",
						Result:   varName,
						Operands: []Value{val},
						Type_:    valType,
					})
				}
			} else if parser.IsSubscript(targetMap) {
				// Handle subscript assignment: arr[i] = value, dict[key] = value
				valueExpr := parser.GetSubscriptValue(targetMap)
				sliceExpr := parser.GetSubscriptSlice(targetMap)

				targetVal, _, err := convertExpression(valueExpr.(map[string]interface{}))
				if err != nil {
					return nil, err
				}

				var indexVal Value
				if sliceMap, ok := sliceExpr.(map[string]interface{}); ok {
					indexVal, _, err = convertExpression(sliceMap)
					if err != nil {
						return nil, err
					}
				}

				assignVal, _, err := convertExpression(value.(map[string]interface{}))
				if err != nil {
					return nil, err
				}

				ops = append(ops, Operation{
					Type:     "assign_subscript",
					Operands: []Value{targetVal, indexVal, assignVal},
				})
			}
		}
	} else if parser.IsExpr(stmt) {
		// Handle expression statement - might be a call
		if value, ok := stmt["value"].(map[string]interface{}); ok {
			val, _, err := convertExpression(value)
			if err != nil {
				return nil, err
			}
			// Store expression as a statement (for side effects like method calls)
			ops = append(ops, Operation{
				Type:  "expr",
				Value: val,
			})
		}
	} else if parser.IsIf(stmt) {
		// Handle if statement with proper block structure
		test := parser.GetIfTest(stmt)
		ifBody := parser.GetIfBody(stmt)
		elseBody := parser.GetIfOrelse(stmt)

		var testVal Value
		var err error
		if testMap, ok := test.(map[string]interface{}); ok {
			testVal, _, err = convertExpression(testMap)
			if err != nil {
				return nil, err
			}
		}

		ifOp := Operation{
			Type:      "if",
			Condition: testVal,
			Body:      make([]Operation, 0),
			ElseBody:  make([]Operation, 0),
			HasElse:   len(elseBody) > 0,
		}

		// Convert if body
		for _, ifStmt := range ifBody {
			if ifStmtMap, ok := ifStmt.(map[string]interface{}); ok {
				ifOps, err := convertStatement(ifStmtMap)
				if err != nil {
					return nil, err
				}
				ifOp.Body = append(ifOp.Body, ifOps...)
			}
		}

		// Convert else body
		for _, elseStmt := range elseBody {
			if elseStmtMap, ok := elseStmt.(map[string]interface{}); ok {
				elseOps, err := convertStatement(elseStmtMap)
				if err != nil {
					return nil, err
				}
				ifOp.ElseBody = append(ifOp.ElseBody, elseOps...)
			}
		}

		ops = append(ops, ifOp)
	} else if parser.IsFor(stmt) {
		// Handle for loop with proper block structure
		target := parser.GetForTarget(stmt)
		iter := parser.GetForIter(stmt)
		body := parser.GetForBody(stmt)

		var targetName string
		var targetNames []string // For tuple unpacking like: for i, row in enumerate(data)
		if targetMap, ok := target.(map[string]interface{}); ok {
			// Check if target is a tuple (for tuple unpacking)
			if nodeType, ok := targetMap["__node_type__"].(string); ok && nodeType == "Tuple" {
				// Handle tuple unpacking: for i, row in enumerate(data)
				if elts, ok := targetMap["elts"].([]interface{}); ok {
					targetNames = make([]string, 0, len(elts))
					for _, elt := range elts {
						if eltMap, ok := elt.(map[string]interface{}); ok {
							targetNames = append(targetNames, parser.GetNameID(eltMap))
						}
					}
				}
			} else {
				targetName = parser.GetNameID(targetMap)
			}
		}

		var iterVal Value
		var err error
		if iterMap, ok := iter.(map[string]interface{}); ok {
			iterVal, _, err = convertExpression(iterMap)
			if err != nil {
				return nil, err
			}
		}

		forOp := Operation{
			Type:     "for",
			Target:   targetName,
			Iterator: iterVal,
			Body:     make([]Operation, 0),
		}

		// Store tuple target names if present
		if len(targetNames) > 0 {
			forOp.Value = targetNames
		}

		// Convert for body
		for _, bodyStmt := range body {
			if bodyStmtMap, ok := bodyStmt.(map[string]interface{}); ok {
				bodyOps, err := convertStatement(bodyStmtMap)
				if err != nil {
					return nil, err
				}
				forOp.Body = append(forOp.Body, bodyOps...)
			}
		}

		ops = append(ops, forOp)
	} else if parser.IsWhile(stmt) {
		// Handle while loop with proper block structure
		test := parser.GetWhileTest(stmt)
		body := parser.GetWhileBody(stmt)

		var testVal Value
		var err error
		if testMap, ok := test.(map[string]interface{}); ok {
			testVal, _, err = convertExpression(testMap)
			if err != nil {
				return nil, err
			}
		}

		whileOp := Operation{
			Type:      "while",
			Condition: testVal,
			Body:      make([]Operation, 0),
		}

		// Convert while body
		for _, bodyStmt := range body {
			if bodyStmtMap, ok := bodyStmt.(map[string]interface{}); ok {
				bodyOps, err := convertStatement(bodyStmtMap)
				if err != nil {
					return nil, err
				}
				whileOp.Body = append(whileOp.Body, bodyOps...)
			}
		}

		ops = append(ops, whileOp)
	} else if parser.IsBreak(stmt) {
		// Handle break statement
		ops = append(ops, Operation{
			Type: "break",
		})
	} else if parser.IsContinue(stmt) {
		// Handle continue statement
		ops = append(ops, Operation{
			Type: "continue",
		})
	} else if parser.IsAugAssign(stmt) {
		// Handle augmented assignment (+=, -=, *=, etc.)
		target := parser.GetAugAssignTarget(stmt)
		op := parser.GetAugAssignOp(stmt)
		value := parser.GetAugAssignValue(stmt)

		if targetMap, ok := target.(map[string]interface{}); ok {
			if parser.IsName(targetMap) {
				varName := parser.GetNameID(targetMap)

				if valueMap, ok := value.(map[string]interface{}); ok {
					val, valType, err := convertExpression(valueMap)
					if err != nil {
						return nil, err
					}
					ops = append(ops, Operation{
						Type:     "aug_assign",
						Result:   varName,
						Operands: []Value{val},
						Type_:    valType,
						Module:   op, // Reuse Module field for the operator
					})
				}
			}
		}
	} else if parser.IsTry(stmt) {
		// Handle try/except/finally
		tryBody := parser.GetTryBody(stmt)
		handlers := parser.GetTryHandlers(stmt)
		elseBody := parser.GetTryOrelse(stmt)
		finallyBody := parser.GetTryFinalbody(stmt)

		tryOp := Operation{
			Type:        "try",
			Body:        make([]Operation, 0),
			Handlers:    make([]ExceptionHandler, 0),
			ElseBody:    make([]Operation, 0),
			FinallyBody: make([]Operation, 0),
		}

		// Convert try body
		for _, tryStmt := range tryBody {
			if tryStmtMap, ok := tryStmt.(map[string]interface{}); ok {
				tryOps, err := convertStatement(tryStmtMap)
				if err != nil {
					return nil, err
				}
				tryOp.Body = append(tryOp.Body, tryOps...)
			}
		}

		// Convert exception handlers
		for _, handler := range handlers {
			if handlerMap, ok := handler.(map[string]interface{}); ok {
				excHandler := ExceptionHandler{
					Body: make([]Operation, 0),
				}

				// Get exception type
				if excType := parser.GetExceptHandlerType(handlerMap); excType != nil {
					if excTypeMap, ok := excType.(map[string]interface{}); ok {
						if parser.IsName(excTypeMap) {
							excHandler.ExceptionType = parser.GetNameID(excTypeMap)
						}
					}
				}

				// Get exception variable name
				excHandler.VarName = parser.GetExceptHandlerName(handlerMap)

				// Convert handler body
				handlerBody := parser.GetExceptHandlerBody(handlerMap)
				for _, hStmt := range handlerBody {
					if hStmtMap, ok := hStmt.(map[string]interface{}); ok {
						hOps, err := convertStatement(hStmtMap)
						if err != nil {
							return nil, err
						}
						excHandler.Body = append(excHandler.Body, hOps...)
					}
				}

				tryOp.Handlers = append(tryOp.Handlers, excHandler)
			}
		}

		// Convert else body (executed if no exception)
		for _, elseStmt := range elseBody {
			if elseStmtMap, ok := elseStmt.(map[string]interface{}); ok {
				elseOps, err := convertStatement(elseStmtMap)
				if err != nil {
					return nil, err
				}
				tryOp.ElseBody = append(tryOp.ElseBody, elseOps...)
			}
		}

		// Convert finally body
		for _, finallyStmt := range finallyBody {
			if finallyStmtMap, ok := finallyStmt.(map[string]interface{}); ok {
				finallyOps, err := convertStatement(finallyStmtMap)
				if err != nil {
					return nil, err
				}
				tryOp.FinallyBody = append(tryOp.FinallyBody, finallyOps...)
			}
		}

		ops = append(ops, tryOp)
	} else if parser.IsRaise(stmt) {
		// Handle raise statement
		exc := parser.GetRaiseExc(stmt)
		var excVal Value
		if exc != nil {
			if excMap, ok := exc.(map[string]interface{}); ok {
				excVal, _, _ = convertExpression(excMap)
			}
		}
		ops = append(ops, Operation{
			Type:     "raise",
			Operands: []Value{excVal},
		})
	}

	return ops, nil
}
