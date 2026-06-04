package commands

import (
	"regexp"
	"strings"
)

const redactedToken = "[REDACTED]"

// sensitiveKeyRe matches "key = value" or "key: value" or "key=value"
// where the key is one of the credential names we know to redact.
// We only redact the value when we recognise the key, so we don't
// accidentally mangle unrelated text.
var sensitiveKeyRe = regexp.MustCompile(
	`(?i)\b(` +
		`token|access_token|refresh_token|id_token|` +
		`authorization|bearer|` +
		`password|passwd|pwd|` +
		`secret|` +
		`api_key|apikey|x-api-key|x-auth-token|` +
		`session|cookie|set-cookie|` +
		`client_secret|private_key|` +
		`ff_token|ff_api_key` +
		`)\b` +
		`\s*[:=]\s*` + // separator
		`([^\s,;\r\n}]+)`) // value up to next whitespace/terminator

// authHeaderRe matches an Authorization / Cookie / Set-Cookie header line
// in its entirety. We rewrite the line to a colon-free marker so the
// sensitiveKeyRe (which would also match "Authorization: ...") doesn't
// double-process it.
var authHeaderRe = regexp.MustCompile(`(?im)^(Authorization|Cookie|Set-Cookie)\s*:\s*.*$`)

// bearerFreeTextRe matches a bare "Bearer <token>" anywhere in free text.
var bearerFreeTextRe = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-+/=]{8,}`)

// RedactString scrubs credential-like substrings out of s. It is safe to
// call on arbitrary user-supplied text — it only redacts when a known
// sensitive key or well-formed header precedes the value.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	// Header lines first, so sensitiveKeyRe (which would also match them)
	// doesn't double-process the line. We use a sentinel (no colon, no
	// equals) so the second pass has nothing to match.
	const sentinel = "__REDACTED_HEADER_SENTINEL__"
	out := authHeaderRe.ReplaceAllString(s, "$1 "+sentinel)
	out = strings.ReplaceAll(out, sentinel, redactedToken)
	out = sensitiveKeyRe.ReplaceAllString(out, "$1="+redactedToken)
	out = bearerFreeTextRe.ReplaceAllString(out, "Bearer "+redactedToken)
	return out
}

// redactArgs redacts string args before they are passed to fmt.Fprintf.
// Non-string args are left alone (so we don't accidentally mangle numbers
// or other structured values).
func redactArgs(args []interface{}) []interface{} {
	if len(args) == 0 {
		return args
	}
	out := make([]interface{}, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			out[i] = RedactString(s)
		} else if err, ok := a.(error); ok {
			out[i] = RedactString(err.Error())
		} else {
			out[i] = a
		}
	}
	return out
}

// redactFormat is a no-op kept for symmetry with redactArgs. The format
// string itself is not redacted because format verbs like %s, %d must be
// preserved literally.
func redactFormat(format string) string { return format }
