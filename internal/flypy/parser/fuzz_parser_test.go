package parser_test

import (
	"context"
	"testing"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

func FuzzParsePython(f *testing.F) {
	seeds := []string{
		"def handler(data):\n    return data",
		"import json\nimport math",
		"def handler(data):\n    return {'ok': True}",
		"from datetime import datetime",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > parser.MaxSourceSize {
			return
		}
		_, _ = parser.ParsePython(context.Background(), src)
	})
}
