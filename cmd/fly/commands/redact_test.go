package commands

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactString_BearerHeader(t *testing.T) {
	in := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig"
	out := RedactString(in)
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("token leaked: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "authorization") {
		t.Errorf("header name lost: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("redaction marker missing: %q", out)
	}
}

func TestRedactString_CookieHeader(t *testing.T) {
	in := "Cookie: session=abc123; id=xyz"
	out := RedactString(in)
	if strings.Contains(out, "abc123") {
		t.Errorf("cookie value leaked: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "cookie") {
		t.Errorf("header name lost: %q", out)
	}
}

func TestRedactString_KeyValuePairs(t *testing.T) {
	cases := []struct {
		in   string
		leak string
		key  string
	}{
		{"token=ff_abcd1234efgh5678", "ff_abcd1234efgh5678", "token"},
		{"password=hunter2", "hunter2", "password"},
		{"api_key=sk_live_abcdefghij", "sk_live_abcdefghij", "api_key"},
		{"Authorization: bearer abcdefghijklmnop", "abcdefghijklmnop", "Authorization"},
		{"refresh_token: rt_12345", "rt_12345", "refresh_token"},
	}
	for _, c := range cases {
		out := RedactString(c.in)
		if strings.Contains(out, c.leak) {
			t.Errorf("%s: value %q leaked in %q", c.key, c.leak, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s: redaction marker missing in %q", c.key, out)
		}
	}
}

func TestRedactString_FreeTextBearer(t *testing.T) {
	in := `sending to server: Bearer abcdefghijklmnop.qrstuv`
	out := RedactString(in)
	if strings.Contains(out, "abcdefghijklmnop") {
		t.Errorf("free-text token leaked: %q", out)
	}
}

func TestRedactString_NoFalsePositives(t *testing.T) {
	cases := []string{
		"hello world",
		"the user name is alice",
		"a token was mentioned in the docs", // "token" without a value
		"timestamp=2025-01-01T00:00:00Z",    // timestamp value, "token" is substring
		"127.0.0.1:8080",                    // has a colon but not a "key: value" pair
		"version: 1.0.0",
		"region: us-east-1",
	}
	for _, c := range cases {
		out := RedactString(c)
		if out != c {
			t.Errorf("false positive on %q → %q", c, out)
		}
	}
}

func TestRedactString_EmptyAndUnicode(t *testing.T) {
	if got := RedactString(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
	// Unicode-heavy text should pass through unmolested.
	in := "🔐 using token=abc123def456 in production"
	out := RedactString(in)
	if !strings.Contains(out, "🔐") {
		t.Errorf("emoji lost: %q", out)
	}
	if strings.Contains(out, "abc123def456") {
		t.Errorf("token value leaked: %q", out)
	}
}

func TestRedactArgs_HandlesErrors(t *testing.T) {
	got := redactArgs([]interface{}{errors.New("password=hunter2")})
	if s, ok := got[0].(string); !ok || strings.Contains(s, "hunter2") {
		t.Errorf("error arg not redacted: %v", got[0])
	}
}

func TestRedactArgs_PassesThroughNonStrings(t *testing.T) {
	got := redactArgs([]interface{}{42, true, 1.5})
	if got[0] != 42 || got[1] != true || got[2] != 1.5 {
		t.Errorf("non-string args mangled: %v", got)
	}
}
