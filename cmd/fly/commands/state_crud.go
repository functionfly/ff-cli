package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type StateEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Size      int    `json:"size,omitempty"`
	TTL       int    `json:"ttl,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type StateListResponse struct {
	Entries []StateEntry `json:"entries"`
	Total   int          `json:"total,omitempty"`
}

// stateBasePath returns the API path prefix for state operations.
func stateBasePath(author, name string) string {
	return fmt.Sprintf("/v1/state/%s/%s", author, name)
}

// resolveStateTarget resolves author/name from args or manifest+creds.
func resolveStateTarget(args []string) (string, string, error) {
	return resolveAuthorName(args)
}

// --- list ---

func newStateListCmd() *cobra.Command {
	var asJSON bool
	var prefix string
	var limit int
	cmd := &cobra.Command{
		Use:     "list [author/name]",
		Aliases: []string{"ls"},
		Short:   "List state keys",
		Long:    `List all state keys for a function. Use --prefix to filter by key prefix.`,
		Example: `  ff state list
  ff state list alice/my-fn
  ff state list --prefix "user:" --limit 50
  ff state list --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateList(args, prefix, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Filter keys by prefix")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum keys to return (1-1000)")
	return cmd
}

func runStateList(args []string, prefix string, limit int, asJSON bool) error {
	author, name, err := resolveStateTarget(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	params := []string{fmt.Sprintf("limit=%d", limit)}
	if prefix != "" {
		params = append(params, "prefix="+prefix)
	}
	path := stateBasePath(author, name) + "?" + strings.Join(params, "&")

	var resp StateListResponse
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not list state: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Entries) == 0 {
		fmt.Printf("No state entries for %s/%s.\n", author, name)
		if prefix != "" {
			fmt.Printf("\nNo keys match prefix %q.\n", prefix)
		} else {
			fmt.Printf("\nSet a value with: ff state set <key> <value>\n")
		}
		return nil
	}

	fmt.Printf("\nState: %s/%s (%d keys)\n\n", author, name, len(resp.Entries))
	fmt.Printf("  %-36s  %-8s  %-6s  %s\n", "KEY", "SIZE", "TTL", "UPDATED")
	fmt.Println("  " + strings.Repeat("-", 72))

	for _, e := range resp.Entries {
		size := formatSize(e.Size)
		ttl := "-"
		if e.TTL > 0 {
			ttl = formatTTL(e.TTL)
		}
		updatedAt := e.UpdatedAt
		if len(updatedAt) > 19 {
			updatedAt = updatedAt[:19]
		}
		if updatedAt == "" {
			updatedAt = "-"
		}
		fmt.Printf("  %-36s  %-8s  %-6s  %s\n", e.Key, size, ttl, updatedAt)
	}

	fmt.Println()
	return nil
}

// --- get ---

func newStateGetCmd() *cobra.Command {
	var asJSON bool
	var raw bool
	cmd := &cobra.Command{
		Use:   "get <key> [author/name]",
		Short: "Get a state value",
		Long: `Get the value for a state key. By default displays as formatted text.
Use --raw to print only the value (for piping).`,
		Example: `  ff state get my-key
  ff state get my-key alice/my-fn
  ff state get my-key --raw | jq .
  ff state get my-key --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateGet(args, asJSON, raw)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print only the value (for piping)")
	return cmd
}

func runStateGet(args []string, asJSON, raw bool) error {
	key := args[0]
	tail := args[1:]
	author, name, err := resolveStateTarget(tail)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var entry StateEntry
	path := stateBasePath(author, name) + "/" + key
	if err := client.Get(path, &entry); err != nil {
		return fmt.Errorf("could not get state key %q: %w", key, err)
	}

	if raw {
		fmt.Println(entry.Value)
		return nil
	}

	if asJSON || WantJSON() {
		printJSON(entry)
		return nil
	}

	fmt.Printf("\n  Key:       %s\n", entry.Key)
	fmt.Printf("  Value:     %s\n", entry.Value)
	if entry.Size > 0 {
		fmt.Printf("  Size:      %s\n", formatSize(entry.Size))
	}
	if entry.TTL > 0 {
		fmt.Printf("  TTL:       %s\n", formatTTL(entry.TTL))
	}
	if entry.UpdatedAt != "" {
		fmt.Printf("  Updated:   %s\n", entry.UpdatedAt)
	} else if entry.CreatedAt != "" {
		fmt.Printf("  Created:   %s\n", entry.CreatedAt)
	}
	fmt.Println()
	return nil
}

// --- set ---

func newStateSetCmd() *cobra.Command {
	var asJSON bool
	var ttl int
	cmd := &cobra.Command{
		Use:   "set <key> <value> [author/name]",
		Short: "Set a state value",
		Long: `Set or update a state key. The value can be any string — use JSON for
structured data. Optionally set a TTL (time-to-live) in seconds.`,
		Example: `  ff state set my-key "hello"
  ff state set counter '{"count": 42}'
  ff state set session-id "abc123" --ttl 3600
  ff state set my-key "value" alice/my-fn --json`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateSet(args[0], args[1], args[2:], ttl, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&ttl, "ttl", 0, "Time-to-live in seconds (0 = no expiry)")
	return cmd
}

func runStateSet(key, value string, tail []string, ttl int, asJSON bool) error {
	author, name, err := resolveStateTarget(tail)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"key":   key,
		"value": value,
	}
	if ttl > 0 {
		body["ttl"] = ttl
	}

	var entry StateEntry
	path := stateBasePath(author, name) + "/" + key
	if err := client.Put(path, body, &entry); err != nil {
		return fmt.Errorf("could not set state key %q: %w", key, err)
	}

	if asJSON || WantJSON() {
		printJSON(entry)
		return nil
	}

	fmt.Printf("✅ %s/%s — %s = %s\n", author, name, key, truncateValue(value, 60))
	if ttl > 0 {
		fmt.Printf("   TTL: %s\n", formatTTL(ttl))
	}
	return nil
}

// --- delete ---

func newStateDeleteCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "delete <key> [author/name]",
		Aliases: []string{"rm", "del"},
		Short:   "Delete a state key",
		Long:    `Delete a state key and its value. Requires confirmation unless --force is used.`,
		Example: `  ff state delete my-key
  ff state delete my-key --force
  ff state delete my-key alice/my-fn --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateDelete(args[0], args[1:], force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runStateDelete(key string, tail []string, force, asJSON bool) error {
	author, name, err := resolveStateTarget(tail)
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Delete state key %q from %s/%s?", key, author, name),
			false,
		)
		if !confirmed {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	path := stateBasePath(author, name) + "/" + key
	if err := client.Delete(path, nil); err != nil {
		return fmt.Errorf("could not delete state key %q: %w", key, err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{"success": true, "key": key, "deleted": true})
		return nil
	}

	fmt.Printf("✅ Deleted %s/%s — %s\n", author, name, key)
	return nil
}

// --- clear ---

func newStateClearCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "clear [author/name]",
		Short: "Clear all state for a function",
		Long: `Delete all state entries for a function. This cannot be undone.
Requires confirmation unless --force is used.`,
		Example: `  ff state clear
  ff state clear --force
  ff state clear alice/my-fn --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateClear(args, force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runStateClear(args []string, force, asJSON bool) error {
	author, name, err := resolveStateTarget(args)
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Clear ALL state for %s/%s? This cannot be undone.", author, name),
			false,
		)
		if !confirmed {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	path := stateBasePath(author, name)
	if err := client.Delete(path, nil); err != nil {
		return fmt.Errorf("could not clear state: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{"success": true, "function": author + "/" + name, "cleared": true})
		return nil
	}

	fmt.Printf("✅ Cleared all state for %s/%s\n", author, name)
	return nil
}

// --- export ---

func newStateExportCmd() *cobra.Command {
	var asJSON bool
	var output string
	cmd := &cobra.Command{
		Use:   "export [author/name]",
		Short: "Export all state to a file",
		Long:  `Export all state entries for a function to a JSON file.`,
		Example: `  ff state export
  ff state export --output state.json
  ff state export alice/my-fn --output backup.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateExport(args, output, asJSON)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "state.json", "Output file path")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output to stdout as JSON instead of file")
	return cmd
}

func runStateExport(args []string, output string, asJSON bool) error {
	author, name, err := resolveStateTarget(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Fetch all entries (may need pagination)
	var resp StateListResponse
	path := stateBasePath(author, name) + "?limit=1000"
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not export state: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	// Build export object
	export := map[string]interface{}{
		"function": author + "/" + name,
		"entries":  resp.Entries,
		"total":    resp.Total,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize state: %w", err)
	}

	if err := writeFileAtomic(output, data, 0600); err != nil {
		return fmt.Errorf("could not write %s: %w", output, err)
	}

	fmt.Printf("✅ Exported %d state entries to %s\n", len(resp.Entries), output)
	return nil
}

// --- import ---

func newStateImportCmd() *cobra.Command {
	var asJSON bool
	var file string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import state from a file",
		Long: `Import state entries from a JSON file (previously exported with
'ff state export'). Existing keys are overwritten.`,
		Example: `  ff state import --file state.json
  ff state import --file backup.json --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateImport(file, asJSON)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "JSON file to import (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runStateImport(file string, asJSON bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", file, err)
	}

	var importData struct {
		Function string       `json:"function"`
		Entries  []StateEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &importData); err != nil {
		return fmt.Errorf("invalid import file: %w", err)
	}

	if len(importData.Entries) == 0 {
		fmt.Println("No entries to import.")
		return nil
	}

	// Parse author/name from the function field or from manifest
	var author, name string
	if importData.Function != "" {
		parts := splitAuthorName(importData.Function)
		if len(parts) == 2 {
			author, name = parts[0], parts[1]
		}
	}
	if author == "" || name == "" {
		var err error
		author, name, err = resolveAuthorName(nil)
		if err != nil {
			return err
		}
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	imported := 0
	failed := 0
	for _, e := range importData.Entries {
		body := map[string]interface{}{
			"key":   e.Key,
			"value": e.Value,
		}
		if e.TTL > 0 {
			body["ttl"] = e.TTL
		}
		path := stateBasePath(author, name) + "/" + e.Key
		if err := client.Put(path, body, nil); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  Failed to import %s: %v\n", e.Key, err)
			failed++
			continue
		}
		imported++
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"function": author + "/" + name,
			"imported": imported,
			"failed":   failed,
			"total":    len(importData.Entries),
		})
		return nil
	}

	fmt.Printf("✅ Imported %d state entries to %s/%s", imported, author, name)
	if failed > 0 {
		fmt.Printf(" (%d failed)", failed)
	}
	fmt.Println()
	return nil
}

// --- helpers ---

func formatSize(bytes int) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func formatTTL(seconds int) string {
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%dd", seconds/86400)
	case seconds >= 3600:
		return fmt.Sprintf("%dh", seconds/3600)
	case seconds >= 60:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func truncateValue(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// readFileLines reads a file and returns its lines (for import variants).
func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
