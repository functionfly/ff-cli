package backend

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// Package backend provides Rust code generation for Python function and method calls.
// It translates Python standard library functions and methods into equivalent Rust code.

// Error constants for consistent error handling
var (
	ErrInvalidArguments = errors.New("invalid number of arguments")
	ErrInvalidOperation = errors.New("invalid operation")
	ErrModuleNotFound   = errors.New("module not found")
	ErrFunctionNotFound = errors.New("function not found")
)

// handleError provides consistent error handling for code generation failures.
// It logs the error and returns a formatted error comment in the generated code.
//
// Parameters:
//   - operation: The name of the operation that failed
//   - err: The error that occurred
//
// Returns:
//   - A string containing an error comment for inclusion in generated code
func handleError(operation string, err error) string {
	log.Printf("Code generation error in %s: %v", operation, err)
	return fmt.Sprintf("    // Error: %s - %v", operation, err)
}

// generateCall is the CodegenContext-aware version of GenerateCall.
// It uses ctx.generateValue for proper parameter scoping.
func (ctx *CodegenContext) generateCall(op ir.Operation) string {
	if op.Value == nil {
		log.Printf("generateCall: operation value is nil")
		return "    // Error: invalid operation - no value"
	}

	callMap, ok := op.Value.(map[string]interface{})
	if !ok {
		log.Printf("generateCall: operation value is not a map: %T", op.Value)
		return "    // Error: invalid operation - expected map"
	}

	fn, ok := callMap["func"].(string)
	if !ok {
		log.Printf("generateCall: function name not found in call map")
		return "    // Error: invalid operation - no function name"
	}

	args, ok := callMap["args"].([]ir.Value)
	if !ok {
		log.Printf("generateCall: args not found or invalid for function %s", fn)
		return fmt.Sprintf("    // Error: invalid arguments for %s()", fn)
	}

	argStrs := make([]string, 0, len(args))
	for _, arg := range args {
		argStrs = append(argStrs, ctx.generateValue(arg))
	}

	return generateCallSwitch(fn, argStrs)
}

// GenerateCall generates Rust code for function calls from Python IR operations.
// It handles built-in Python functions like len(), abs(), str(), etc., and delegates
// to specialized generators for module-specific functions.
//
// Parameters:
//   - op: The IR operation containing the function call information
//
// Returns:
//   - A string containing the equivalent Rust code for the function call
//   - Error comments in the generated code if the operation is invalid
func GenerateCall(op ir.Operation) string {
	ctx := newCodegenContext()
	return ctx.generateCall(op)
}

// generateCallSwitch contains the actual switch logic for built-in function calls.
// Extracted to avoid duplication between GenerateCall and generateCall.
func generateCallSwitch(fn string, argStrs []string) string {
	// Generate Rust-equivalent calls
	switch fn {
	case "len":
		if len(argStrs) == 0 {
			return handleError("len", ErrInvalidArguments)
		}
		receiver := argStrs[0]
		// For serde_json::Value, we need to handle len differently
		if isJsonValueVariable(receiver) {
			return fmt.Sprintf("    %s.as_array().map(|arr| arr.len()).unwrap_or(0)", receiver)
		}
		return fmt.Sprintf("    %s.len()", receiver)
	case "abs":
		if len(argStrs) == 0 {
			return handleError("abs", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s.abs()", argStrs[0])
	case "min":
		if len(argStrs) == 0 {
			return handleError("min", ErrInvalidArguments)
		}
		// Use unwrap_or_default() to avoid panic on empty slices
		return fmt.Sprintf("    %s.iter().min().cloned().unwrap_or_default()", argStrs[0])
	case "max":
		if len(argStrs) == 0 {
			return handleError("max", ErrInvalidArguments)
		}
		// Use unwrap_or_default() to avoid panic on empty slices
		return fmt.Sprintf("    %s.iter().max().cloned().unwrap_or_default()", argStrs[0])
	case "sum":
		if len(argStrs) == 0 {
			return handleError("sum", ErrInvalidArguments)
		}
		// Annotate the result type so Rust can infer the iterator's item
		// type when the receiver's type isn't fully constrained (e.g. when
		// summing a literal-init vec![]).
		return fmt.Sprintf("    %s.iter().sum::<i32>()", argStrs[0])
	case "str":
		if len(argStrs) == 0 {
			return handleError("str", ErrInvalidArguments)
		}
		// Exception variables are already rewritten to "exception".to_string() in generateValue.
		// Avoid double-wrapping with format! so output is the string, not the literal.
		if argStrs[0] == "\"exception\".to_string()" {
			return "    \"exception\".to_string()"
		}
		return fmt.Sprintf("    format!(\"{}\", %s)", argStrs[0])
	case "int":
		if len(argStrs) == 0 {
			return handleError("int", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s.parse::<i32>().unwrap_or(0)", argStrs[0])
	case "float":
		if len(argStrs) == 0 {
			return handleError("float", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s.parse::<f64>().unwrap_or(0.0)", argStrs[0])
	case "bool":
		if len(argStrs) == 0 {
			return handleError("bool", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s != \"\" && %s != \"0\" && %s.to_lowercase() != \"false\"", argStrs[0], argStrs[0], argStrs[0])
	case "any":
		if len(argStrs) == 0 {
			return handleError("any", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s.iter().any(|x| *x)", argStrs[0])
	case "all":
		if len(argStrs) == 0 {
			return handleError("all", ErrInvalidArguments)
		}
		return fmt.Sprintf("    %s.iter().all(|x| *x)", argStrs[0])
	case "re.sub":
		// re.sub(pattern, replacement, string)
		if len(argStrs) < 3 {
			return handleError("re.sub", ErrInvalidArguments)
		}
		return fmt.Sprintf("    re_sub(%s, %s, %s)", argStrs[0], argStrs[1], argStrs[2])
	case "re.compile":
		// re.compile(pattern) - use a safe fallback that won't panic
		if len(argStrs) == 0 {
			return handleError("re.compile", ErrInvalidArguments)
		}
		// Use a safe pattern: if the regex fails to compile, fall back to a pattern that matches nothing
		return fmt.Sprintf("    Regex::new(%s).unwrap_or_else(|e| { eprintln!(\"Regex compile error: {}\", e); Regex::new(\"(?!)\").unwrap() })", argStrs[0])
	default:
		log.Printf("generateCall: unknown function %s with args %v", fn, argStrs)
		return fmt.Sprintf("    // Error: unknown function %s(%s)", fn, strings.Join(argStrs, ", "))
	}
}

// GenerateModuleCall generates Rust code for module function calls without keyword arguments.
// This is a convenience wrapper around GenerateModuleCallWithKwargs.
//
// Parameters:
//   - module: The Python module name (e.g., "csv", "json", "re")
//   - fn: The function name within the module
//   - argStrs: Array of argument strings in Rust syntax
//
// Returns:
//   - A string containing the equivalent Rust code for the module function call
func GenerateModuleCall(module, fn string, argStrs []string) string {
	return GenerateModuleCallWithKwargs(module, fn, argStrs, nil)
}

// GenerateModuleCallWithKwargs generates Rust code for module function calls with keyword arguments.
// It supports common Python standard library modules including csv, json, re, hashlib, and base64.
//
// Supported modules and functions:
//   - csv: reader, writer, DictWriter
//   - json: loads, dumps
//   - re: match, search, sub, findall, split
//   - hashlib: sha256, md5
//   - base64: encode, decode
//
// Parameters:
//   - module: The Python module name (e.g., "csv", "json", "re")
//   - fn: The function name within the module
//   - argStrs: Array of positional argument strings in Rust syntax
//   - kwargs: Map of keyword argument names to their Rust syntax values
//
// Returns:
//   - A string containing the equivalent Rust code for the module function call
//   - Error comments if the module/function combination is not supported
func GenerateModuleCallWithKwargs(module, fn string, argStrs []string, kwargs map[string]string) string {
	switch module {
	case "csv":
		switch fn {
		case "reader":
			if len(argStrs) == 0 {
				return handleError("csv.reader", ErrInvalidArguments)
			}
			if delimiter, ok := kwargs["delimiter"]; ok {
				// Convert delimiter to char: extract first char from string, default to ','
				delimExpr := fmt.Sprintf("%s.as_str().unwrap_or(\",\").chars().next().unwrap_or(',')", delimiter)
				return fmt.Sprintf("csv_reader_with_delimiter(%s, %s).unwrap_or_else(|e| { eprintln!(\"CSV reader error: {}\", e); vec![] })", argStrs[0], delimExpr)
			}
			return fmt.Sprintf("csv_reader(%s).unwrap_or_else(|e| { eprintln!(\"CSV reader error: {}\", e); vec![] })", argStrs[0])
		case "writer":
			if len(argStrs) == 0 {
				return handleError("csv.writer", ErrInvalidArguments)
			}
			return fmt.Sprintf("csv_writer(%s).unwrap_or_else(|e| { eprintln!(\"CSV writer error: {{}}\", e); String::new() })", argStrs[0])
		case "DictReader":
			// csv.DictReader(input, **kwargs) -> returns iterator of dicts
			if len(argStrs) == 0 {
				return handleError("csv.DictReader", ErrInvalidArguments)
			}

			// Build options from kwargs
			var optionsBuilder strings.Builder
			optionsBuilder.WriteString("CsvOptions::new()")

			// Handle delimiter kwarg
			if delimiter, ok := kwargs["delimiter"]; ok {
				// Extract first character from string literal
				if strings.HasPrefix(delimiter, "\"") && strings.HasSuffix(delimiter, "\"") && len(delimiter) >= 3 {
					char := delimiter[1:2] // Get first char after opening quote
					optionsBuilder.WriteString(fmt.Sprintf(".with_delimiter('%s')", char))
				}
			}

			// Handle quotechar kwarg
			if quotechar, ok := kwargs["quotechar"]; ok {
				if strings.HasPrefix(quotechar, "\"") && strings.HasSuffix(quotechar, "\"") && len(quotechar) >= 3 {
					char := quotechar[1:2]
					optionsBuilder.WriteString(fmt.Sprintf(".with_quote('%s')", char))
				}
			}

			// Handle fieldnames kwarg (for header override)
			hasFieldnames := false
			if _, ok := kwargs["fieldnames"]; ok {
				hasFieldnames = true
				optionsBuilder.WriteString(".has_headers(false)")
			}

			// Handle other options
			if skipinitialspace, ok := kwargs["skipinitialspace"]; ok && skipinitialspace == "true" {
				optionsBuilder.WriteString(".skip_initial_space(true)")
			}

			// Handle encoding
			if encoding, ok := kwargs["encoding"]; ok {
				if strings.HasPrefix(encoding, "\"") && strings.HasSuffix(encoding, "\"") {
					enc := strings.Trim(encoding, "\"")
					if enc == "latin1" || enc == "iso-8859-1" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Latin1)")
					} else if enc == "windows-1252" || enc == "cp1252" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Windows1252)")
					} else if enc == "utf-16" || enc == "utf16" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf16Le)")
					} else if enc == "utf-16le" || enc == "utf16le" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf16Le)")
					} else if enc == "utf-16be" || enc == "utf16be" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf16Be)")
					} else if enc == "utf-32" || enc == "utf32" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf32Le)")
					} else if enc == "utf-32le" || enc == "utf32le" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf32Le)")
					} else if enc == "utf-32be" || enc == "utf32be" {
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf32Be)")
					} else {
						// Default to UTF-8 for unknown encodings
						optionsBuilder.WriteString(".with_encoding(Encoding::Utf8)")
					}
				}
			}

			// Handle type inference mode
			if typeInference, ok := kwargs["type_inference"]; ok {
				if typeInference == "\"none\"" {
					optionsBuilder.WriteString(".with_type_inference(TypeInferenceMode::None)")
				} else if typeInference == "\"aggressive\"" {
					optionsBuilder.WriteString(".with_type_inference(TypeInferenceMode::Aggressive)")
				} else {
					// Default to basic
					optionsBuilder.WriteString(".with_type_inference(TypeInferenceMode::Basic)")
				}
			}

			// Handle max records
			if maxRecords, ok := kwargs["max_records"]; ok {
				if strings.HasPrefix(maxRecords, "\"") && strings.HasSuffix(maxRecords, "\"") {
					maxStr := strings.Trim(maxRecords, "\"")
					if max, err := strconv.Atoi(maxStr); err == nil {
						optionsBuilder.WriteString(fmt.Sprintf(".with_max_records(%d)", max))
					}
				}
			}

			// If the argument is StringIO::from_string(...), extract the inner string
			arg := argStrs[0]
			if strings.HasPrefix(arg, "StringIO::from_string(") && strings.HasSuffix(arg, ")") {
				// Extract the inner argument: StringIO::from_string(inner) -> inner
				inner := arg[len("StringIO::from_string(") : len(arg)-1]

				if hasFieldnames {
					// For custom fieldnames, use from_string_with_auto_delimiter but override headers
					return fmt.Sprintf("CsvDictReader::from_string_with_auto_delimiter(&%s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", inner)
				} else {
					// Use custom options
					optionsStr := optionsBuilder.String()
					if optionsStr == "CsvOptions::new()" {
						return fmt.Sprintf("CsvDictReader::new(&%s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", inner)
					} else {
						return fmt.Sprintf("CsvDictReader::new_with_options(&%s, %s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", inner, optionsStr)
					}
				}
			}

			// For direct string arguments
			if hasFieldnames {
				return fmt.Sprintf("CsvDictReader::from_string_with_auto_delimiter(%s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", argStrs[0])
			} else {
				optionsStr := optionsBuilder.String()
				if optionsStr == "CsvOptions::new()" {
					return fmt.Sprintf("CsvDictReader::new(%s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", argStrs[0])
				} else {
					return fmt.Sprintf("CsvDictReader::new_with_options(%s, %s).unwrap_or_else(|e| { eprintln!(\"CSV DictReader error: {}\", e); CsvDictReader::new(\"\").unwrap() })", argStrs[0], optionsStr)
				}
			}
		case "DictWriter":
			// csv.DictWriter(output, fieldnames=...) -> create a CSV dict writer
			// For now, return a simple struct that can write rows
			var output string
			if len(argStrs) > 0 {
				output = argStrs[0]
			} else {
				output = "StringIO::new()"
			}
			// Check for fieldnames kwarg
			if fieldnames, ok := kwargs["fieldnames"]; ok {
				return fmt.Sprintf("CsvDictWriter::new(%s).with_fieldnames(%s)", output, fieldnames)
			}
			return fmt.Sprintf("CsvDictWriter::new(%s)", output)
		}
	case "io":
		switch fn {
		case "StringIO":
			if len(argStrs) > 0 {
				return fmt.Sprintf("StringIO::from_string(%s)", argStrs[0])
			}
			return "StringIO::new()"
		case "BytesIO":
			return "BytesIO::new()"
		}
	case "json":
		switch fn {
		case "loads":
			if len(argStrs) == 0 {
				return handleError("json.loads", ErrInvalidArguments)
			}
			// Check if the argument is a str() call result (format! call)
			if strings.Contains(argStrs[0], "format!(\"{}\", ") {
				// This produces a String, so use json_loads_str
				return fmt.Sprintf("json_loads_str(&%s)", argStrs[0])
			} else if strings.HasPrefix(argStrs[0], "\"") && strings.HasSuffix(argStrs[0], "\"") {
				// String literal - parse it directly
				return fmt.Sprintf("json_loads_str(%s)", argStrs[0])
			} else {
				// Assume it's already a serde_json::Value
				return fmt.Sprintf("json_loads(&%s)", argStrs[0])
			}
		case "dumps":
			if len(argStrs) == 0 {
				return handleError("json.dumps", ErrInvalidArguments)
			}
			return fmt.Sprintf("serde_json::to_string(%s).unwrap_or_else(|e| { eprintln!(\"JSON dumps error: {{}}\", e); String::new() })", argStrs[0])
		}
	case "re":
		switch fn {
		case "match":
			if len(argStrs) < 2 {
				return handleError("re.match", ErrInvalidArguments)
			}
			return fmt.Sprintf("re_match(%s, %s)", argStrs[0], argStrs[1])
		case "search":
			if len(argStrs) < 2 {
				return handleError("re.search", ErrInvalidArguments)
			}
			return fmt.Sprintf("re_search(%s, %s)", argStrs[0], argStrs[1])
		case "sub":
			if len(argStrs) < 3 {
				return handleError("re.sub", ErrInvalidArguments)
			}
			return fmt.Sprintf("re_sub(%s, %s, %s).unwrap_or_else(|e| { eprintln!(\"Regex sub error: {{}}\", e); String::new() })", argStrs[0], argStrs[1], argStrs[2])
		case "findall":
			if len(argStrs) < 2 {
				return handleError("re.findall", ErrInvalidArguments)
			}
			return fmt.Sprintf("re_findall(%s, %s).unwrap_or_else(|e| { eprintln!(\"Regex findall error: {{}}\", e); vec![] })", argStrs[0], argStrs[1])
		case "split":
			if len(argStrs) < 2 {
				return handleError("re.split", ErrInvalidArguments)
			}
			return fmt.Sprintf("re_split(%s, %s).unwrap_or_else(|e| { eprintln!(\"Regex split error: {{}}\", e); vec![] })", argStrs[0], argStrs[1])
		}
	case "hashlib":
		switch fn {
		case "sha256":
			if len(argStrs) > 0 {
				return fmt.Sprintf("hash_sha256(%s)", argStrs[0])
			}
		case "md5":
			if len(argStrs) > 0 {
				return fmt.Sprintf("hash_md5(%s)", argStrs[0])
			}
		}
	case "base64":
		switch fn {
		case "encode":
			if len(argStrs) == 0 {
				return handleError("base64.encode", ErrInvalidArguments)
			}
			return fmt.Sprintf("base64_encode(%s)", argStrs[0])
		case "decode":
			if len(argStrs) == 0 {
				return handleError("base64.decode", ErrInvalidArguments)
			}
			return fmt.Sprintf("base64_decode(%s).unwrap_or_else(|e| { eprintln!(\"Base64 decode error: {{}}\", e); vec![] })", argStrs[0])
		}
	}
	return fmt.Sprintf("/* unknown module: %s.%s */", module, fn)
}

// GenerateMethodCall generates Rust code for method calls on objects.
// It handles common Python methods for strings, lists, dictionaries, and IO objects,
// translating them to equivalent Rust operations.
//
// Supported methods:
//   - IO methods: write, read, getvalue, seek, tell, close
//   - List methods: append, pop
//   - String methods: split, strip, upper, lower, replace, startswith, endswith, join
//   - Dict methods: keys, values, items, get
//
// Parameters:
//   - receiver: The variable name of the object receiving the method call (in Rust syntax)
//   - method: The method name being called
//   - argStrs: Array of argument strings in Rust syntax
//   - movedVariables: optional map of original variable name -> new name (e.g. after CsvDictWriter::new(io))
//
// Returns:
//   - A string containing the equivalent Rust code for the method call
//   - Error comments if the method is not supported or arguments are invalid
func GenerateMethodCall(receiver, method string, argStrs []string, movedVariables ...map[string]string) string {
	if receiver == "" {
		return handleError("method call", errors.New("receiver is empty"))
	}
	if method == "" {
		return handleError("method call", errors.New("method name is empty"))
	}

	var mv map[string]string
	if len(movedVariables) > 0 {
		mv = movedVariables[0]
	}
	// Check if the receiver has been moved to another variable
	if mv != nil {
		if newReceiver, ok := mv[receiver]; ok {
			receiver = newReceiver
		}
	}

	// Common methods for StringIO/BytesIO
	switch method {
	case "write":
		if len(argStrs) == 0 {
			return fmt.Sprintf("%s.write(\"\")", receiver)
		}
		return fmt.Sprintf("%s.write(%s)", receiver, argStrs[0])
	case "read":
		return fmt.Sprintf("%s.read()", receiver)
	case "getvalue":
		return fmt.Sprintf("%s.getvalue()", receiver)
	case "seek":
		if len(argStrs) == 0 {
			return fmt.Sprintf("%s.seek(0)", receiver)
		}
		return fmt.Sprintf("%s.seek(%s)", receiver, argStrs[0])
	case "tell":
		return fmt.Sprintf("%s.tell()", receiver)
	case "close":
		return fmt.Sprintf("%s.close()", receiver)
	case "append":
		if len(argStrs) == 0 {
			return handleError("append", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.push(%s)", receiver, argStrs[0])
	case "pop":
		return fmt.Sprintf("%s.pop().unwrap_or_default()", receiver)
	case "writerows":
		// writerows expects a reference since it takes &Value
		if len(argStrs) == 0 {
			return handleError("writerows", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.writerows(&%s).unwrap_or_else(|e| { eprintln!(\"CSV writerows error: {}\", e); })", receiver, argStrs[0])
	case "writeheader":
		// writeheader() - no arguments expected for CsvDictWriter
		if len(argStrs) > 0 {
			return handleError("writeheader", errors.New("writeheader takes no arguments"))
		}
		return fmt.Sprintf("%s.writeheader()", receiver)
	case "writerow":
		// writerow(row_dict) - expects a dictionary/serde_json::Value
		if len(argStrs) == 0 {
			return handleError("writerow", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.writerow(&%s).unwrap_or_else(|e| { eprintln!(\"CSV writerow error: {}\", e); })", receiver, argStrs[0])
	case "keys":
		// For serde_json::Value, we need to use as_object() first
		// Optimization: avoid unwrap() panic, return empty vec on error
		return fmt.Sprintf("%s.as_object().map(|obj| obj.keys().cloned().collect::<Vec<String>>()).unwrap_or_else(|| vec![])", receiver)
	case "values":
		// Optimization: avoid unnecessary clone if values are being consumed
		return fmt.Sprintf("%s.as_object().map(|obj| obj.values().cloned().collect::<Vec<serde_json::Value>>()).unwrap_or_else(|| vec![])", receiver)
	case "items":
		// Optimization: avoid unnecessary clones by using references where possible
		return fmt.Sprintf("%s.as_object().map(|obj| obj.iter().map(|(k, v)| (k.clone(), v.clone())).collect::<Vec<(String, serde_json::Value)>>()).unwrap_or_else(|| vec![])", receiver)
	case "split":
		if len(argStrs) == 0 {
			// Optimization: collect into Vec<&str> to avoid allocations when possible
			return fmt.Sprintf("%s.split_whitespace().collect::<Vec<&str>>()", receiver)
		}
		return fmt.Sprintf("%s.split(%s).collect::<Vec<&str>>()", receiver, argStrs[0])
	case "strip":
		// Handle both String and serde_json::Value inputs
		if isJsonValueVariable(receiver) {
			return fmt.Sprintf("%s.as_str().unwrap_or(\"\").trim().to_string()", receiver)
		}
		return fmt.Sprintf("%s.trim().to_string()", receiver)
	case "truth":
		// Check if string is non-empty (used for "if not s.strip():")
		if isJsonValueVariable(receiver) {
			return fmt.Sprintf("!%s.as_str().unwrap_or(\"\").trim().is_empty()", receiver)
		}
		return fmt.Sprintf("!%s.trim().is_empty()", receiver)
	case "upper":
		return fmt.Sprintf("%s.to_uppercase()", receiver)
	case "lower":
		return fmt.Sprintf("%s.to_lowercase()", receiver)
	case "replace":
		if len(argStrs) < 2 {
			return handleError("replace", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.replace(%s, %s)", receiver, argStrs[0], argStrs[1])
	case "startswith":
		if len(argStrs) == 0 {
			return handleError("startswith", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.starts_with(%s)", receiver, argStrs[0])
	case "endswith":
		if len(argStrs) == 0 {
			return handleError("endswith", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.ends_with(%s)", receiver, argStrs[0])
	case "get":
		// dict.get(key) or dict.get(key, default)
		// For serde_json::Map, we need to handle this specially
		if len(argStrs) == 0 {
			return handleError("get", ErrInvalidArguments)
		}

		if len(argStrs) >= 2 {
			// dict.get(key, default) - return default if key not found
			defaultVal := argStrs[1]

			// Check if it's a simple literal that needs wrapping in serde_json::Value
			if strings.HasPrefix(defaultVal, "vec![") {
				// Convert vec![...] to serde_json::Value::Array(vec![...])
				defaultVal = fmt.Sprintf("serde_json::Value::Array(%s)", defaultVal)
			} else if strings.HasPrefix(defaultVal, "\"") && strings.HasSuffix(defaultVal, "\"") && !strings.Contains(defaultVal, "(") {
				// It's a simple string literal (no function calls) - convert to serde_json::Value::String
				defaultVal = fmt.Sprintf("serde_json::Value::String(%s)", defaultVal)
			} else if defaultVal == "true" {
				// Boolean true literal
				defaultVal = "serde_json::Value::Bool(true)"
			} else if defaultVal == "false" {
				// Boolean false literal
				defaultVal = "serde_json::Value::Bool(false)"
			} else if defaultVal == "vec![]" {
				defaultVal = "serde_json::Value::Array(vec![])"
			} else if len(defaultVal) > 0 && defaultVal[0] >= '0' && defaultVal[0] <= '9' && !strings.Contains(defaultVal, "(") {
				// Numeric literal (no function calls) - check for float (contains .) or integer
				if strings.Contains(defaultVal, ".") {
					// Float literal
					defaultVal = fmt.Sprintf("serde_json::Value::Number(serde_json::Number::from_f64(%s).unwrap_or(serde_json::Number::from(0)))", defaultVal)
				} else {
					// Integer literal
					defaultVal = fmt.Sprintf("serde_json::Value::Number(serde_json::Number::from(%s))", defaultVal)
				}
			} else {
				// It's an expression that evaluates to the right type - assume it needs to be converted to serde_json::Value
				// For complex expressions, we need to wrap them appropriately
				if strings.Contains(defaultVal, ".to_string()") {
					// String expression - convert to serde_json::Value::String
					defaultVal = fmt.Sprintf("serde_json::Value::String(%s)", defaultVal)
				} else if strings.Contains(defaultVal, "vec!") {
					// Array expression - convert to serde_json::Value::Array
					defaultVal = fmt.Sprintf("serde_json::Value::Array(%s)", defaultVal)
				} else {
					// For other expressions, assume they're already the right serde_json::Value
					// This handles cases like serde_json::Value::Null, etc.
					_ = defaultVal
				}
			}
			return fmt.Sprintf("%s.get(%s).cloned().unwrap_or(%s)", receiver, argStrs[0], defaultVal)
		}
		// dict.get(key) - return null if key not found
		return fmt.Sprintf("%s.get(%s).cloned().unwrap_or(serde_json::Value::Null)", receiver, argStrs[0])
	case "join":
		// str.join(iterable)
		// Python: ",".join(headers) means use "," as separator to join headers
		// Rust: headers.join(",") - the join method is on the vector, not the separator
		// Optimization: avoid unnecessary to_string() calls if already strings
		if len(argStrs) == 0 {
			return handleError("join", ErrInvalidArguments)
		}
		return fmt.Sprintf("%s.iter().map(|s| s.as_ref()).collect::<Vec<&str>>().join(%s)", argStrs[0], receiver)
	case "format":
		// str.format(*args)
		if len(argStrs) == 0 {
			return handleError("format", ErrInvalidArguments)
		}
		return fmt.Sprintf("format!(\"{}\", %s)", strings.Join(argStrs, ", "))
	}

	// Default method call
	return fmt.Sprintf("%s.%s(%s)", receiver, method, strings.Join(argStrs, ", "))
}

func isJsonValueVariable(expr string) bool {
	// Only variables that are definitely serde_json::Value that could be arrays
	// (not a method call, not a literal, not already an iterator)
	if strings.Contains(expr, "(") || strings.Contains(expr, "[") {
		return false
	}
	// Check if it's a known parameter or variable that would be a JSON value
	// input.* fields are serde_json::Value
	if strings.HasPrefix(expr, "input.") {
		return true
	}
	// Variables that come from input
	if strings.HasPrefix(expr, "input_") {
		return true
	}
	// Only treat specific known variables as JSON values that might be arrays
	knownJsonArrayVars := []string{"data"}
	for _, v := range knownJsonArrayVars {
		if expr == v {
			return true
		}
	}
	return false
}
