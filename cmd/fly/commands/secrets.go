package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Short:   "Manage secrets",
		Example: "  ff secrets list\n  ff secrets set API_KEY=sk-abc123\n  ff secrets unset API_KEY",
	}
	cmd.AddCommand(newSecretsListCmd(), newSecretsSetCmd(), newSecretsUnsetCmd())
	return cmd
}

func newSecretsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List secret names (values are hidden)",
		RunE: func(cmd *cobra.Command, args []string) error { return runSecretsList(asJSON) },
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newSecretsSetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "set KEY=value [KEY=value ...]", Short: "Set one or more secrets",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runSecretsSet(args, dryRun) },
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	return cmd
}

func newSecretsUnsetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "unset KEY [KEY ...]", Aliases: []string{"delete", "rm"}, Short: "Remove one or more secrets",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runSecretsUnset(args, dryRun) },
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	return cmd
}

type SecretInfo struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func runSecretsList(asJSON bool) error {
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
	var secrets []SecretInfo
	path := fmt.Sprintf("/v1/registry/%s/%s/secrets", creds.User.Username, manifest.Name)
	if err := client.Get(path, &secrets); err != nil {
		return fmt.Errorf("could not fetch secrets: %w", err)
	}
	if asJSON {
		printJSON(secrets)
		return nil
	}
	if len(secrets) == 0 {
		fmt.Println("No secrets set.")
		fmt.Println("   → Use: ff secrets set KEY=value")
		return nil
	}
	fmt.Printf("Secrets for %s/%s:\n\n", creds.User.Username, manifest.Name)
	for _, s := range secrets {
		fmt.Printf("  🔒 %s\n", s.Name)
	}
	fmt.Printf("\n%d secret(s) — values are hidden\n", len(secrets))
	return nil
}

func runSecretsSet(pairs []string, dryRun bool) error {
	if dryRun {
		fmt.Println("Dry run — would set the following secrets:")
		for _, p := range pairs {
			parts := strings.SplitN(p, "=", 2)
			if len(parts) < 1 {
				continue
			}
			fmt.Printf("  %s=<redacted>\n", parts[0])
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
	secrets := map[string]string{}
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format %q — expected KEY=value", pair)
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return fmt.Errorf("empty key in %q", pair)
		}
		secrets[key] = parts[1]
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/registry/%s/%s/secrets", creds.User.Username, manifest.Name)
	if err := client.Put(path, secrets, nil); err != nil {
		return fmt.Errorf("could not set secrets: %w", err)
	}
	for k := range secrets {
		fmt.Printf("✅ Set secret %s\n", k)
	}
	fmt.Println("\n⚠️  Secrets are encrypted and cannot be retrieved after being set.")
	return nil
}

func runSecretsUnset(keys []string, dryRun bool) error {
	if dryRun {
		fmt.Println("Dry run — would remove the following secrets:")
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
		path := fmt.Sprintf("/v1/registry/%s/%s/secrets/%s", creds.User.Username, manifest.Name, key)
		if err := client.Delete(path, nil); err != nil {
			return fmt.Errorf("could not unset secret %s: %w", key, err)
		}
		fmt.Printf("✅ Removed secret %s\n", key)
	}
	return nil
}
