package commands

import (
	"bufio"
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
		Example: "  ff env list\n  ff env set KEY=value\n  ff env get KEY\n  ff env unset KEY\n  ff env apply          # read .env and set variables\n  ff env apply --dry-run",
	}
	cmd.AddCommand(newEnvListCmd(), newEnvSetCmd(), newEnvGetCmd(), newEnvUnsetCmd(), newEnvApplyCmd())
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
