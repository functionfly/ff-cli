package backend

import (
	"fmt"
	"strings"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// GenerateRust generates Rust code from IR (backward compatible)
func GenerateRust(module *ir.Module) (string, error) {
	return GenerateRustWithMode(module, "deterministic")
}

// GenerateRustWithMode generates Rust code from IR based on execution mode
func GenerateRustWithMode(module *ir.Module, mode string) (string, error) {
	if len(module.Functions) == 0 {
		return "", fmt.Errorf("no functions in module")
	}

	// Get the main handler function
	handler := module.Functions[0]

	// Generate input fields from parameters
	inputFields := make([]Field, 0)
	for _, param := range handler.Parameters {
		inputFields = append(inputFields, Field{
			Name:         param.Name,
			Type:         GoTypeToRust(param.Type),
			DefaultValue: GetDefaultValue(param.Type),
		})
	}

	// Generate output fields (based on return type)
	outputFields := []Field{
		{Name: "result", Type: "String", DefaultValue: "String::new()"},
	}

	// Generate the function body
	body := GenerateFunctionBody(handler)

	data := map[string]interface{}{
		"InputFields":  inputFields,
		"OutputFields": outputFields,
		"Body":         body,
	}

	var builder strings.Builder
	var err error

	// Select template based on mode
	switch mode {
	case "complex":
		err = ComplexModeRustTemplate.Execute(&builder, data)
	case "compatible":
		// Compatible mode uses complex template with full runtime
		err = ComplexModeRustTemplate.Execute(&builder, data)
	default:
		// Deterministic mode uses minimal template
		err = DeterministicModeRustTemplate.Execute(&builder, data)
	}

	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return builder.String(), nil
}
