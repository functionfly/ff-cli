package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
)

const (
	// MaxContentSize defines the maximum size of content that can be hashed
	// to prevent memory exhaustion attacks
	MaxContentSize = 100 * 1024 * 1024 // 100MB

	// MaxStringLength defines the maximum length of strings that can be processed
	// to prevent excessive memory usage in string operations
	MaxStringLength = 10 * 1024 * 1024 // 10MB
)

// HashContent generates a SHA256 hash of the provided content.
// Returns an empty string if content is nil or exceeds maximum size.
// This function is designed to be fast and memory-efficient for bundling operations.
func HashContent(content []byte) string {
	if content == nil {
		logrus.WithField("operation", "HashContent").Debug("Received nil content, returning empty hash")
		return ""
	}

	if len(content) == 0 {
		logrus.WithField("operation", "HashContent").Debug("Received empty content, returning empty hash")
		return ""
	}

	if len(content) > MaxContentSize {
		logrus.WithFields(logrus.Fields{
			"content_size": len(content),
			"max_allowed":  MaxContentSize,
			"operation":    "HashContent",
		}).Error("Content exceeds maximum allowed size for hashing")
		return ""
	}

	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// ValidateContent checks if the content is valid for processing.
// Returns an error if content is nil, empty, or too large.
func ValidateContent(content []byte) error {
	if content == nil {
		return errors.New("content cannot be nil")
	}

	if len(content) == 0 {
		return errors.New("content cannot be empty")
	}

	if len(content) > MaxContentSize {
		return errors.New("content exceeds maximum allowed size")
	}

	return nil
}

// escapeForWAT escapes a string for safe use in WebAssembly Text (WAT) data sections.
// WAT data sections require proper escaping of special characters to maintain syntax validity.
// This function handles the most common escape sequences needed for WAT compatibility.
//
// The function escapes:
// - Backslashes (\) → (\\)
// - Double quotes (") → (\")
// - Newlines (\n) → (\n) - kept as-is for readability
// - Carriage returns (\r) → (\r) - kept as-is for readability
// - Tabs (\t) → (\t) - kept as-is for readability
//
// Note: WAT supports UTF-8 strings, so most Unicode characters are safe.
// This escaping focuses on WAT syntax characters that could break parsing.
//
// Returns an empty string if input exceeds maximum allowed length.
func escapeForWAT(s string) string {
	// Input validation: check string length to prevent memory exhaustion
	if len(s) > MaxStringLength {
		logrus.WithFields(logrus.Fields{
			"string_length": len(s),
			"max_allowed":   MaxStringLength,
			"operation":     "escapeForWAT",
		}).Error("String exceeds maximum allowed length for WAT escaping")
		return ""
	}

	if s == "" {
		return s
	}

	// Validate UTF-8 to prevent malformed string issues
	if !utf8.ValidString(s) {
		logrus.WithFields(logrus.Fields{
			"string_length": len(s),
			"operation":     "escapeForWAT",
		}).Warn("Invalid UTF-8 sequence detected in string, replacing invalid bytes with '?'")

		// Convert invalid UTF-8 sequences to valid ones using '?' as replacement
		// This maintains WAT syntax validity while preserving as much content as possible
		s = strings.ToValidUTF8(s, "?")

		// Log the first few bytes of the cleaned string for debugging
		if len(s) > 0 {
			preview := s
			if len(s) > 50 {
				preview = s[:50] + "..."
			}
			logrus.WithField("preview", preview).Debug("UTF-8 cleaned string preview")
		}
	}

	// WAT data sections use parentheses for syntax, but we primarily need to escape
	// quotes, backslashes, and control characters that could interfere with string literals
	result := strings.ReplaceAll(s, "\\", "\\\\")
	result = strings.ReplaceAll(result, "\"", "\\\"")
	result = strings.ReplaceAll(result, "\n", "\\n")
	result = strings.ReplaceAll(result, "\r", "\\r")
	result = strings.ReplaceAll(result, "\t", "\\t")

	return result
}

// SanitizeFileName creates a safe filename by removing or replacing unsafe characters.
// This is useful for generating bundle filenames from potentially unsafe input.
func SanitizeFileName(filename string) string {
	if filename == "" {
		return "unnamed"
	}

	// Replace unsafe characters with underscores
	unsafeChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename

	for _, char := range unsafeChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// Remove multiple consecutive underscores
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}

	// Trim leading/trailing underscores and whitespace
	result = strings.Trim(result, "_ \t\n\r")

	// Ensure we have a valid filename
	if result == "" {
		return "unnamed"
	}

	return result
}
