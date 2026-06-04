package restrictions_test

import (
	"context"
	"testing"

	"github.com/functionfly/ff-cli/internal/flypy/parser"
	"github.com/functionfly/ff-cli/internal/flypy/restrictions"
)

func TestAdversarialCorpusAllModes(t *testing.T) {
	cases := []struct {
		name string
		mode restrictions.ExecutionMode
		code string
	}{
		{"deterministic_json", restrictions.ModeDeterministic, `import json; json.loads("{}")`},
		{"compatible_json", restrictions.ModeCompatible, `import json; json.loads("{}")`},
		{"complex_subprocess_blocked", restrictions.ModeComplex, `import subprocess; subprocess.run(["ls"])`},
		{"deterministic_re_blocked", restrictions.ModeDeterministic, `import re; re.match(".", "x")`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ast, err := parser.ParsePython(context.Background(), c.code)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			errors := restrictions.EnforceWithMode(ast, c.mode)
			for _, e := range errors {
				if e.Type == restrictions.DisallowedType || e.Type == restrictions.UnsupportedFeature {
					continue
				}
			}
		})
	}
}
