package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "env",
		Short:   "Manage environment variables",
		Example: "  ff env list\n  ff env set KEY=value\n  ff env get KEY\n  ff env unset KEY\n  ff env apply          # read .env and set variables\n  ff env apply --dry-run\n  ff env import --format json config.json\n  ff env import --format shell env.sh",
	}
	cmd.AddCommand(newEnvListCmd(), newEnvSetCmd(), newEnvGetCmd(), newEnvUnsetCmd(), newEnvApplyCmd(), newEnvImportCmd())
	return cmd
}

func newEnvListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List all environment variables",
		RunE: func(cmd *cobra.Command, args []string) error { return runEnvList(asJSON) },
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvSetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "set KEY=value [KEY=value ...]", Short: "Set one or more environment variables",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runEnvSet(args, dryRun) },
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	return cmd
}

func newEnvGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "get KEY", Short: "Get the value of an environment variable",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runEnvGet(args[0]) },
	}
}

func newEnvUnsetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "unset KEY [KEY ...]", Aliases: []string{"delete", "rm"}, Short: "Remove one or more environment variables",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runEnvUnset(args, dryRun) },
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	return cmd
}

func newEnvApplyCmd() *cobra.Command {
	var dryRun bool
	var path string
	cmd := &cobra.Command{
		Use:   "apply [path]",
		Short: "Set environment variables from a .env file",
		Long: `Read key=value pairs from a .env file (or a custom path) and set them.
Each line in the file must be KEY=value. Lines starting with # are treated as comments.
Use --dry-run to preview the changes without applying them.`,
		Example: `  ff env apply             # reads .env in current directory
  ff env apply .env.staging
  ff env apply --dry-run
  ff env apply --path /path/to/.env`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := path
			if p == "" && len(args) > 0 {
				p = args[0]
			}
			return runEnvApply(p, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	cmd.Flags().StringVar(&path, "path", "", "Path to .env file (default: .env in current directory)")
	return cmd
}

func runEnvList(asJSON bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var envVars map[string]string
	path := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)
	if err := client.Get(path, &envVars); err != nil {
		return fmt.Errorf("could not fetch environment variables: %w", err)
	}
	if asJSON {
		printJSON(envVars)
		return nil
	}
	if len(envVars) == 0 {
		fmt.Println("No environment variables set.")
		fmt.Println("   → Use: ff env set KEY=value")
		return nil
	}
	fmt.Printf("Environment variables for %s/%s:\n\n", creds.User.Username, manifest.Name)
	for k := range envVars {
		fmt.Printf("  %s\n", k)
	}
	fmt.Printf("\n%d variable(s) — values hidden; use 'ff env get KEY' to view a specific value\n", len(envVars))
	return nil
}

func runEnvSet(pairs []string, dryRun bool) error {
	envVars := map[string]string{}
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format %q — expected KEY=value", pair)
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return fmt.Errorf("empty key in %q", pair)
		}
		envVars[key] = parts[1]
	}
	if dryRun {
		fmt.Println("Dry run — would set:")
		for k, v := range envVars {
			fmt.Printf("  %s=%s\n", k, v)
		}
		fmt.Printf("\nRun without --dry-run to apply.\n")
		return nil
	}
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)
	if err := client.Put(path, envVars, nil); err != nil {
		return fmt.Errorf("could not set environment variables: %w", err)
	}
	for k := range envVars {
		fmt.Printf("  %s (set)\n", k)
	}
	return nil
}

func runEnvGet(key string) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var envVars map[string]string
	path := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)
	if err := client.Get(path, &envVars); err != nil {
		return fmt.Errorf("could not fetch environment variables: %w", err)
	}
	value, ok := envVars[key]
	if !ok {
		return fmt.Errorf("environment variable %q not found\n   → Use 'ff env list' to see all variables", key)
	}
	fmt.Println(value)
	return nil
}

func runEnvUnset(keys []string, dryRun bool) error {
	if dryRun {
		fmt.Println("Dry run — would unset:")
		for _, k := range keys {
			fmt.Printf("  %s\n", k)
		}
		fmt.Printf("\nRun without --dry-run to apply.\n")
		return nil
	}
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	for _, key := range keys {
		path := fmt.Sprintf("/v1/registry/%s/%s/env/%s", creds.User.Username, manifest.Name, key)
		if err := client.Delete(path, nil); err != nil {
			return fmt.Errorf("could not unset %s: %w", key, err)
		}
		fmt.Printf("✅ Unset %s\n", key)
	}
	return nil
}

func runEnvApply(envPath string, dryRun bool) error {
	if envPath == "" {
		envPath = ".env"
	}
	absEnv, err := filepath.Abs(envPath)
	if err != nil {
		return fmt.Errorf("could not resolve env path %q: %w", envPath, err)
	}
	if info, err := os.Stat(absEnv); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("env file not found: %s\n   → Use --path to specify a different file, or create one in the current directory", absEnv)
		}
		return fmt.Errorf("could not access %s: %w", absEnv, err)
	} else if info.IsDir() {
		return fmt.Errorf("%s is a directory, expected an env file\n   → Use --path to specify a different file", absEnv)
	}
	if li, err := os.Lstat(absEnv); err == nil && li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to read symlinked env file: %s", absEnv)
	}
	f, err := os.Open(envPath)
	if err != nil {
		return fmt.Errorf("could not open %s: %w\n   → Use --path to specify a different file", envPath, err)
	}
	defer f.Close()

	pairs := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Handle export prefix
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			fmt.Printf("  ⚠️  Skipping line %d (invalid format): %s\n", lineNum, line)
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := parts[1]
		// Strip surrounding quotes
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		pairs[key] = value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading %s: %w", envPath, err)
	}

	if len(pairs) == 0 {
		fmt.Println("No variables found in", envPath)
		return nil
	}

	if dryRun {
		fmt.Printf("Dry run — would set %d variable(s) from %s:\n\n", len(pairs), filepath.Base(envPath))
		keys := make([]string, 0, len(pairs))
		for k := range pairs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s=%s\n", k, pairs[k])
		}
		fmt.Printf("\n%d variable(s)\nRun without --dry-run to apply.\n", len(pairs))
		return nil
	}

	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Fetch existing vars to diff
	var existing map[string]string
	path := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)
	if err := client.Get(path, &existing); err != nil {
		existing = map[string]string{}
	}

	if err := client.Put(path, pairs, nil); err != nil {
		return fmt.Errorf("could not apply environment variables: %w", err)
	}

	newCount, updCount := 0, 0
	appliedKeys := make([]string, 0, len(pairs))
	for k := range pairs {
		if _, ok := existing[k]; ok {
			updCount++
		} else {
			newCount++
		}
		appliedKeys = append(appliedKeys, k)
	}
	sort.Strings(appliedKeys)
	for _, k := range appliedKeys {
		fmt.Printf("  %s (set)\n", k)
	}
	fmt.Printf("\nApplied %d variable(s) from %s (%d new, %d updated)\n", len(pairs), filepath.Base(envPath), newCount, updCount)
	return nil
}

func newEnvImportCmd() *cobra.Command {
	var format string
	var dryRun bool
	var override bool
	cmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import environment variables from a file",
		Long: `Import environment variables from a file in one of several formats.

Supported formats:
  dotenv   — KEY=value lines (default for .env files)
  json     — {"KEY": "value"} object
  yaml     — KEY: value YAML mapping
  shell    — export KEY=value lines

Use --override to replace all existing variables with the imported set
(default is to merge, only adding or updating).`,
		Example: `  ff env import config.json --format json
  ff env import .env.staging --format dotenv
  ff env import env.sh --format shell
  ff env import vars.yaml --format yaml
  ff env import config.json --format json --override --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvImport(args[0], format, dryRun, override)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "File format: dotenv, json, yaml, shell (auto-detected from extension if omitted)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	cmd.Flags().BoolVar(&override, "override", false, "Replace all existing variables instead of merging")
	return cmd
}

func runEnvImport(filePath, format string, dryRun, override bool) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("could not resolve path %q: %w", filePath, err)
	}
	if info, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", absPath)
		}
		return fmt.Errorf("could not access %s: %w", absPath, err)
	} else if info.IsDir() {
		return fmt.Errorf("%s is a directory, expected a file", absPath)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", absPath, err)
	}

	if format == "" {
		format = detectFormat(absPath)
	}

	pairs, err := parseEnvFile(data, format)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		fmt.Printf("No variables found in %s (format: %s)\n", filepath.Base(absPath), format)
		return nil
	}

	action := "merge"
	if override {
		action = "override"
	}

	if dryRun {
		fmt.Printf("Dry run — would %s %d variable(s) from %s (%s):\n\n", action, len(pairs), filepath.Base(absPath), format)
		keys := make([]string, 0, len(pairs))
		for k := range pairs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s=%s\n", k, pairs[k])
		}
		fmt.Printf("\nRun without --dry-run to apply.\n")
		return nil
	}

	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	envPath := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)

	var existing map[string]string
	if err := client.Get(envPath, &existing); err != nil {
		existing = map[string]string{}
	}

	if override {
		if err := client.Put(envPath, pairs, nil); err != nil {
			return fmt.Errorf("could not import environment variables: %w", err)
		}
	} else {
		merged := map[string]string{}
		for k, v := range existing {
			merged[k] = v
		}
		for k, v := range pairs {
			merged[k] = v
		}
		if err := client.Put(envPath, merged, nil); err != nil {
			return fmt.Errorf("could not import environment variables: %w", err)
		}
	}

	newCount, updCount := 0, 0
	for k := range pairs {
		if _, ok := existing[k]; ok {
			updCount++
		} else {
			newCount++
		}
	}

	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s (set)\n", k)
	}
	fmt.Printf("\nImported %d variable(s) from %s (%s) — %d new, %d updated\n", len(pairs), filepath.Base(absPath), action, newCount, updCount)
	return nil
}

func detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".sh":
		return "shell"
	default:
		return "dotenv"
	}
}

func parseEnvFile(data []byte, format string) (map[string]string, error) {
	switch format {
	case "dotenv":
		return parseDotenv(data), nil
	case "json":
		return parseEnvJSON(data)
	case "yaml":
		return parseEnvYAML(data)
	case "shell":
		return parseShell(data), nil
	default:
		return nil, fmt.Errorf("unsupported format %q — use dotenv, json, yaml, or shell", format)
	}
}

func parseDotenv(data []byte) map[string]string {
	pairs := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
			v = v[1 : len(v)-1]
		}
		pairs[key] = v
	}
	return pairs
}

func parseEnvJSON(data []byte) (map[string]string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	pairs := map[string]string{}
	for k, v := range raw {
		pairs[k] = fmt.Sprintf("%v", v)
	}
	return pairs, nil
}

func parseEnvYAML(data []byte) (map[string]string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML (must be a flat key: value mapping): %w", err)
	}
	pairs := map[string]string{}
	for k, v := range raw {
		pairs[k] = fmt.Sprintf("%v", v)
	}
	return pairs, nil
}

func parseShell(data []byte) map[string]string {
	pairs := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
			v = v[1 : len(v)-1]
		}
		pairs[key] = v
	}
	return pairs
}
