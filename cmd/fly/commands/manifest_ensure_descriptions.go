package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// NewManifestCmd returns the manifest command.
func NewManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Manifest utilities",
	}
	cmd.AddCommand(NewManifestEnsureDescriptionsCmd())
	return cmd
}

// NewManifestEnsureDescriptionsCmd returns the ensure-descriptions subcommand.
func NewManifestEnsureDescriptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ensure-descriptions [directory]",
		Short: "Add description to each functionfly.jsonc when missing (humanized from name)",
		Long:  `Reads each functionfly.jsonc under the directory, and when "description" is missing or empty, sets it from the function name (e.g. text-truncate → "Text truncate"). Comments, formatting, and key order in the JSONC source are preserved.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			return runManifestEnsureDescriptions(dir)
		},
	}
}

func runManifestEnsureDescriptions(baseDir string) error {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base dir: %w", err)
	}
	if li, err := os.Lstat(absBase); err == nil && li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to traverse symlinked base directory: %s", absBase)
	}
	dirs, err := findFunctionDirs(absBase, "")
	if err != nil {
		return fmt.Errorf("find manifests: %w", err)
	}
	if len(dirs) == 0 {
		fmt.Printf("No functionfly.jsonc found under %s\n", absBase)
		return nil
	}
	updated := 0
	for _, d := range dirs {
		if li, err := os.Lstat(d); err == nil && li.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "  skip %s: symlinks are not supported\n", d)
			continue
		}
		cleanDir, err := safeWritePath(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", d, err)
			continue
		}
		manifestPath := filepath.Join(cleanDir, "functionfly.jsonc")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", manifestPath, err)
			continue
		}
		name, desc, hasDescription, parseErr := parseManifestNameAndDescription(raw)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", manifestPath, parseErr)
			continue
		}
		if name == "" {
			fmt.Fprintf(os.Stderr, "  skip %s: no name\n", manifestPath)
			continue
		}
		if hasDescription {
			if len(desc) > 500 {
				fmt.Fprintf(os.Stderr, "  ⚠️  %s: description is %d chars (max 500). Shorten it for the registry.\n", name, len(desc))
			}
			continue
		}
		newRaw, err := injectDescriptionIntoJSONC(raw, humanizeFunctionName(name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fail %s: %v\n", manifestPath, err)
			continue
		}
		if err := os.WriteFile(manifestPath, newRaw, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "  fail %s: %v\n", manifestPath, err)
			continue
		}
		fmt.Printf("  ✓ %s → description: %q\n", name, humanizeFunctionName(name))
		updated++
	}
	fmt.Printf("Updated %d manifest(s) with description.\n", updated)
	return nil
}

// parseManifestNameAndDescription returns the manifest's name, the current
// description (if any), and whether a non-empty description is already
// present. It uses the comment-stripped form for parsing, so JSONC comments
// don't trip it up.
func parseManifestNameAndDescription(raw []byte) (name, desc string, hasDescription bool, err error) {
	cleaned := stripJSONCComments(raw)
	var m map[string]interface{}
	if err := json.Unmarshal(cleaned, &m); err != nil {
		return "", "", false, fmt.Errorf("invalid JSON: %w", err)
	}
	if n, ok := m["name"].(string); ok {
		name = n
	}
	if d, ok := m["description"].(string); ok {
		desc = d
		if strings.TrimSpace(d) != "" {
			hasDescription = true
		}
	}
	return name, desc, hasDescription, nil
}

// descriptionLineRE matches a JSONC "description" key (with optional comment).
// Used as a safety check to avoid inserting a duplicate field.
var descriptionLineRE = regexp.MustCompile(`(?m)^\s*"?description"?\s*:`)

// injectDescriptionIntoJSONC inserts a "description": "value" line into raw
// JSONC source, preserving comments, formatting, and key order. It does not
// reformat or rewrite anything else.
func injectDescriptionIntoJSONC(raw []byte, value string) ([]byte, error) {
	if descriptionLineRE.Match(raw) {
		return raw, nil
	}

	needle := []byte("\"name\"")
	idx := bytes.Index(raw, needle)
	if idx < 0 {
		return nil, fmt.Errorf("no name field found to anchor insertion")
	}

	// Find the indentation of the line containing the "name" key.
	lineStart := bytes.LastIndexByte(raw[:idx], '\n') + 1
	indent := raw[lineStart:idx]
	indentStr := string(indent)

	// Find the end of the "name" value. Strings are the only supported type for
	// "name", so look for the closing quote (respecting escape sequences).
	rest := raw[idx+len(needle):]
	rest = bytes.TrimLeft(rest, " \t:")
	if len(rest) == 0 || rest[0] != '"' {
		return nil, fmt.Errorf("name field is not a string")
	}
	i := 1
	for i < len(rest) {
		if rest[i] == '\\' && i+1 < len(rest) {
			i += 2
			continue
		}
		if rest[i] == '"' {
			break
		}
		i++
	}
	if i >= len(rest) {
		return nil, fmt.Errorf("unterminated name value")
	}
	valEnd := idx + len(needle) + len(raw[idx+len(needle):]) - len(rest) + i + 1

	// Skip an optional inline // comment on the same line.
	scan := valEnd
	for scan < len(raw) && (raw[scan] == ' ' || raw[scan] == '\t') {
		scan++
	}
	if scan+1 < len(raw) && raw[scan] == '/' && raw[scan+1] == '/' {
		nl := bytes.IndexByte(raw[scan:], '\n')
		if nl < 0 {
			// Comment is the last thing in the file — unusual, but bail.
			return nil, fmt.Errorf("name line ends with a comment; please move the description manually")
		}
		valEnd = scan + nl
	}

	descLine := fmt.Sprintf("%s\"description\": %q", indentStr, value)
	hasTrailingComma := false
	scan = valEnd
	for scan < len(raw) && (raw[scan] == ' ' || raw[scan] == '\t') {
		scan++
	}
	if scan < len(raw) && raw[scan] == ',' {
		hasTrailingComma = true
	}

	if hasTrailingComma {
		// "name": "x",\n  "next": ...
		// Insert new "description" key on the line after "name".
		nl := bytes.IndexByte(raw[scan:], '\n')
		if nl < 0 {
			return nil, fmt.Errorf("name is the last field with no newline; please add a newline after the name value")
		}
		insertAt := scan + nl + 1
		out := append([]byte(nil), raw[:insertAt]...)
		out = append(out, []byte(descLine+",\n")...)
		out = append(out, raw[insertAt:]...)
		return out, nil
	}

	// No trailing comma — "name" is the last field before the closing brace.
	// Find the end of the line and append "," + new line with the description.
	lineEnd := valEnd
	for lineEnd < len(raw) && raw[lineEnd] != '\n' {
		lineEnd++
	}
	out := append([]byte(nil), raw[:lineEnd]...)
	out = append(out, ',')
	out = append(out, '\n')
	out = append(out, []byte(descLine+"\n")...)
	out = append(out, raw[lineEnd:]...)
	return out, nil
}
