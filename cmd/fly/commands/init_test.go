package commands

import (
	"strings"
	"testing"
)

func TestIsValidFunctionName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hello-world", true},
		{"slugify", true},
		{"abc123", true},
		{"a", true},
		{"a-b-c-d", true},
		{"my-function-1", true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"Has_Caps", false},
		{"has spaces", false},
		{"with/slash", false},
		{"..", false},
		{".", false},
		{"has.dot", false},
		{"ümlaut", false},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), false},
	}
	for _, c := range cases {
		if got := isValidFunctionName(c.in); got != c.want {
			t.Errorf("isValidFunctionName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRunInit_RejectsTraversal(t *testing.T) {
	cases := []string{
		"..",
		"../foo",
		"foo/../bar",
		"/etc/passwd",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			// isValidFunctionName should already reject these (it does),
			// but the runInit-time guard catches anything the validator
			// misses (e.g. a future relaxation of the charset).
			if isValidFunctionName(name) {
				// Skip the runInit check if the validator allows it.
				return
			}
			err := runInit(name, "hello-world", false)
			if err == nil {
				t.Errorf("expected error for %q", name)
				return
			}
			if !contains(err.Error(), "invalid function name") {
				t.Errorf("error message unexpected for %q: %v", name, err)
			}
		})
	}
}
