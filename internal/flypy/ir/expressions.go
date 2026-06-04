package ir

import (
	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

// convertExpression converts a Python AST expression to an IR Value
func convertExpression(expr map[string]interface{}) (Value, IRType, error) {
	if parser.IsConstant(expr) {
		val := parser.GetConstantValue(expr)
		irType := inferType(val)
		return Value{
			Type:  irType,
			Kind:  Literal,
			Value: val,
		}, irType, nil
	}

	if parser.IsName(expr) {
		name := parser.GetNameID(expr)
		return Value{
			Type:  IRTypeUnknown,
			Kind:  Reference,
			Value: name,
		}, IRTypeUnknown, nil
	}

	if parser.IsBinOp(expr) {
		op, left, right := parser.GetBinOpInfo(expr)
		leftVal, leftType, err := convertExpression(left.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}
		rightVal, rightType, err := convertExpression(right.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}

		// If either operand's type is unknown (e.g. a dict access returns
		// serde_json::Value), the result type is also unknown. This forces
		// the code generator to coerce at the arithmetic site.
		if leftType.Base == "unknown" || rightType.Base == "unknown" {
			return Value{
				Type: IRTypeUnknown,
				Kind: BinOp,
				Value: map[string]interface{}{
					"op":    op,
					"left":  leftVal,
					"right": rightVal,
				},
			}, IRTypeUnknown, nil
		}

		resultType := IRTypeInt
		if leftType.Base == "float" || rightType.Base == "float" {
			resultType = IRTypeFloat
		}

		return Value{
			Type: resultType,
			Kind: BinOp,
			Value: map[string]interface{}{
				"op":    op,
				"left":  leftVal,
				"right": rightVal,
			},
		}, resultType, nil
	}

	if parser.IsCall(expr) {
		funcName := parser.GetCallFunc(expr)
		args := parser.GetCallArgs(expr)
		keywords := parser.GetCallKeywords(expr)

		operands := make([]Value, 0)
		for _, arg := range args {
			if argMap, ok := arg.(map[string]interface{}); ok {
				val, _, err := convertExpression(argMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				operands = append(operands, val)
			}
		}

		// Convert keyword arguments
		kwargs := make(map[string]Value)
		for _, kw := range keywords {
			if kw.Value != nil {
				if valMap, ok := kw.Value.(map[string]interface{}); ok {
					val, _, err := convertExpression(valMap)
					if err == nil {
						kwargs[kw.Arg] = val
					}
				}
			}
		}

		// Check if this is a method call (obj.method())
		// The funcName from GetCallFunc for Attribute calls returns just the attr name
		// We need to check the func node to see if it's an Attribute
		if funcNode, ok := expr["func"].(map[string]interface{}); ok {
			if parser.IsAttribute(funcNode) {
				// This is a method call like obj.method() or module.function()
				receiver := parser.GetAttributeValue(funcNode)
				methodName := parser.GetAttributeAttr(funcNode)

				// Check if the receiver is a module name (e.g., io, csv, json, re)
				if receiverMap, ok := receiver.(map[string]interface{}); ok && parser.IsName(receiverMap) {
					receiverName := parser.GetNameID(receiverMap)
					if isKnownModule(receiverName) {
						// This is a module call like io.StringIO(), csv.DictWriter()
						returnType := inferModuleCallReturnType(receiverName, methodName)
						value := Value{
							Type: returnType,
							Kind: ModuleCall,
							Value: map[string]interface{}{
								"module": receiverName,
								"func":   methodName,
								"args":   operands,
								"kwargs": kwargs,
							},
						}
						// Set CanUnwrap flag for io.StringIO - can be unwrapped to string
						// when passed to functions like csv.DictReader
						if receiverName == "io" && methodName == "StringIO" {
							value.CanUnwrap = true
							value.UnwrapTo = "string"
						}
						return value, returnType, nil
					}
				}

				receiverVal, _, err := convertExpression(receiver.(map[string]interface{}))
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}

				return Value{
					Type: IRTypeUnknown,
					Kind: MethodCall,
					Value: map[string]interface{}{
						"receiver": receiverVal,
						"method":   methodName,
						"args":     operands,
					},
				}, IRTypeUnknown, nil
			}

			// Check if it's a module call (module.function())
			if parser.IsName(funcNode) {
				fullName := parser.GetNameID(funcNode)
				// Check for known module patterns
				module, funcPart := splitModuleFunction(fullName)
				if module != "" {
					return Value{
						Type: IRTypeUnknown,
						Kind: ModuleCall,
						Value: map[string]interface{}{
							"module": module,
							"func":   funcPart,
							"args":   operands,
						},
					}, IRTypeUnknown, nil
				}
			}
		}

		// Regular function call - try to infer the return type
		returnType := inferTypeFromExpr(expr)
		return Value{
			Type: returnType,
			Kind: Call,
			Value: map[string]interface{}{
				"func": funcName,
				"args": operands,
			},
		}, returnType, nil
	}

	if parser.IsSubscript(expr) {
		value := parser.GetSubscriptValue(expr)
		slice := parser.GetSubscriptSlice(expr)

		valueVal, _, err := convertExpression(value.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}

		var sliceVal Value
		if sliceMap, ok := slice.(map[string]interface{}); ok {
			// Check if this is a Slice node (for arr[1:3] syntax)
			if parser.IsSlice(sliceMap) {
				sliceVal, _, err = convertSliceExpression(sliceMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
			} else {
				// Regular index expression
				sliceVal, _, err = convertExpression(sliceMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
			}
		}

		return Value{
			Type: IRTypeUnknown,
			Kind: Subscript,
			Value: map[string]interface{}{
				"value": valueVal,
				"index": sliceVal,
			},
		}, IRTypeUnknown, nil
	}

	if parser.IsAttribute(expr) {
		attr := parser.GetAttributeAttr(expr)
		value := parser.GetAttributeValue(expr)

		valueVal, _, err := convertExpression(value.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}

		return Value{
			Type: IRTypeUnknown,
			Kind: Reference,
			Value: map[string]interface{}{
				"attr":  attr,
				"value": valueVal,
			},
		}, IRTypeUnknown, nil
	}

	if parser.IsCompare(expr) {
		ops := parser.GetCompareOps(expr)
		left := parser.GetCompareLeft(expr)
		comparators := parser.GetCompareComparators(expr)

		leftVal, _, err := convertExpression(left.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}

		compVals := make([]Value, 0)
		for _, comp := range comparators {
			if compMap, ok := comp.(map[string]interface{}); ok {
				compVal, _, err := convertExpression(compMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				compVals = append(compVals, compVal)
			}
		}

		return Value{
			Type: IRTypeBool,
			Kind: Compare,
			Value: map[string]interface{}{
				"ops":         ops,
				"left":        leftVal,
				"comparators": compVals,
			},
		}, IRTypeBool, nil
	}

	if parser.IsBoolOp(expr) {
		op := parser.GetBoolOpOp(expr)
		values := parser.GetBoolOpValues(expr)

		valList := make([]Value, 0)
		for _, v := range values {
			if vMap, ok := v.(map[string]interface{}); ok {
				val, _, err := convertExpression(vMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				valList = append(valList, val)
			}
		}

		return Value{
			Type: IRTypeBool,
			Kind: BoolOp,
			Value: map[string]interface{}{
				"op":     op,
				"values": valList,
			},
		}, IRTypeBool, nil
	}

	if parser.IsUnaryOp(expr) {
		op := parser.GetUnaryOpOp(expr)
		operand := parser.GetUnaryOpOperand(expr)

		operandVal, operandType, err := convertExpression(operand.(map[string]interface{}))
		if err != nil {
			return Value{}, IRTypeUnknown, err
		}

		return Value{
			Type: operandType,
			Kind: UnaryOp,
			Value: map[string]interface{}{
				"op":      op,
				"operand": operandVal,
			},
		}, operandType, nil
	}

	if parser.IsDict(expr) {
		keys := parser.GetDictKeys(expr)
		values := parser.GetDictValues(expr)

		keyList := make([]Value, 0)
		valList := make([]Value, 0)

		for _, k := range keys {
			if kMap, ok := k.(map[string]interface{}); ok {
				keyVal, _, err := convertExpression(kMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				keyList = append(keyList, keyVal)
			}
		}

		for _, v := range values {
			if vMap, ok := v.(map[string]interface{}); ok {
				valVal, _, err := convertExpression(vMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				valList = append(valList, valVal)
			}
		}

		// Infer value type from the values
		elementType := IRTypeString // Default
		if len(valList) > 0 {
			elementType = valList[0].Type
		}
		dictType := IRType{Base: "dict", Key: &IRTypeString, Element: &elementType}

		return Value{
			Type: dictType,
			Kind: Dict,
			Value: map[string]interface{}{
				"keys":   keyList,
				"values": valList,
			},
		}, dictType, nil
	}

	if parser.IsList(expr) {
		elts := parser.GetListElts(expr)

		eltsList := make([]Value, 0)
		for _, e := range elts {
			if eMap, ok := e.(map[string]interface{}); ok {
				eltVal, _, err := convertExpression(eMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				eltsList = append(eltsList, eltVal)
			}
		}

		// Infer element type from the elements
		elementType := IRTypeString // Default
		if len(eltsList) > 0 {
			elementType = eltsList[0].Type
		}
		listType := IRType{Base: "list", Element: &elementType}

		return Value{
			Type: listType,
			Kind: List,
			Value: map[string]interface{}{
				"elements": eltsList,
			},
		}, listType, nil
	}

	// Handle f-strings: f"col_{i}"
	if parser.IsJoinedStr(expr) {
		parts := parser.GetJoinedStrValues(expr)
		fstringParts := make([]Value, 0)
		for _, part := range parts {
			if partMap, ok := part.(map[string]interface{}); ok {
				if parser.IsFormattedValue(partMap) {
					innerExpr := parser.GetFormattedValueExpr(partMap)
					if innerMap, ok := innerExpr.(map[string]interface{}); ok {
						val, _, err := convertExpression(innerMap)
						if err != nil {
							return Value{}, IRTypeUnknown, err
						}
						fstringParts = append(fstringParts, val)
					}
				} else if parser.IsConstant(partMap) {
					val, _, err := convertExpression(partMap)
					if err != nil {
						return Value{}, IRTypeUnknown, err
					}
					fstringParts = append(fstringParts, val)
				}
			}
		}
		return Value{
			Type: IRTypeString,
			Kind: FString,
			Value: map[string]interface{}{
				"parts": fstringParts,
			},
		}, IRTypeString, nil
	}

	// Handle list comprehensions: [x*2 for x in items if x > 0]
	if parser.IsListComp(expr) {
		elt := parser.GetListCompElt(expr)
		generators := parser.GetListCompGenerators(expr)

		// Convert element expression
		var eltVal Value
		var err error
		if eltMap, ok := elt.(map[string]interface{}); ok {
			eltVal, _, err = convertExpression(eltMap)
			if err != nil {
				return Value{}, IRTypeUnknown, err
			}
		}

		// Convert generators (usually just one for simple comprehensions)
		genList := make([]map[string]interface{}, 0)
		for _, gen := range generators {
			genInfo := make(map[string]interface{})

			// Get target (loop variable)
			target := parser.GetCompGenTarget(gen)
			if targetMap, ok := target.(map[string]interface{}); ok {
				genInfo["target"] = parser.GetNameID(targetMap)
			}

			// Get iterator
			iter := parser.GetCompGenIter(gen)
			if iterMap, ok := iter.(map[string]interface{}); ok {
				iterVal, _, err := convertExpression(iterMap)
				if err != nil {
					return Value{}, IRTypeUnknown, err
				}
				genInfo["iterator"] = iterVal
			}

			// Get if conditions (filters)
			ifs := parser.GetCompGenIfs(gen)
			ifConditions := make([]Value, 0)
			for _, ifCond := range ifs {
				if ifMap, ok := ifCond.(map[string]interface{}); ok {
					ifVal, _, err := convertExpression(ifMap)
					if err != nil {
						return Value{}, IRTypeUnknown, err
					}
					ifConditions = append(ifConditions, ifVal)
				}
			}
			genInfo["conditions"] = ifConditions

			genList = append(genList, genInfo)
		}

		return Value{
			Type: IRTypeList,
			Kind: ListComp,
			Value: map[string]interface{}{
				"element":    eltVal,
				"generators": genList,
			},
		}, IRTypeList, nil
	}

	return Value{
		Type:  IRTypeUnknown,
		Kind:  Literal,
		Value: nil,
	}, IRTypeUnknown, nil
}
