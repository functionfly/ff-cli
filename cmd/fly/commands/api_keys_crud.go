package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type APIKey struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	KeyPrefix string   `json:"key_prefix,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	LastUsed  string   `json:"last_used,omitempty"`
	Revoked   bool     `json:"revoked,omitempty"`
}

type APIKeyCreated struct {
	APIKey
	Key string `json:"key"`
}

type APIKeyListResponse struct {
	Keys  []APIKey `json:"keys"`
	Total int      `json:"total,omitempty"`
}

func newAPIKeysListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List API keys",
		Long:    `List all API keys for the current user or tenant.`,
		Example: `  ff api-keys list
  ff api-keys list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIKeysList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAPIKeysList(asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var resp APIKeyListResponse
	if err := client.Get("/v1/api-keys", &resp); err != nil {
		return fmt.Errorf("could not list API keys: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Keys) == 0 {
		fmt.Println("No API keys found.")
		fmt.Println("\nCreate one with: ff api-keys create --name <name>")
		return nil
	}

	fmt.Printf("\nAPI Keys (%d)\n\n", len(resp.Keys))
	fmt.Printf("  %-12s  %-24s  %-12s  %-10s  %s\n", "ID", "NAME", "PREFIX", "SCOPES", "EXPIRES")
	fmt.Println("  " + strings.Repeat("-", 80))

	for _, k := range resp.Keys {
		id := k.ID
		if len(id) > 10 {
			id = id[:10]
		}
		prefix := k.KeyPrefix
		if prefix == "" {
			prefix = "-"
		}
		scopes := "-"
		if len(k.Scopes) > 0 {
			scopes = strings.Join(k.Scopes, ",")
		}
		expires := k.ExpiresAt
		if len(expires) > 10 {
			expires = expires[:10]
		}
		if expires == "" {
			expires = "never"
		}
		status := ""
		if k.Revoked {
			status = " (revoked)"
		}
		fmt.Printf("  %-12s  %-24s  %-12s  %-10s  %s%s\n", id, k.Name, prefix, scopes, expires, status)
	}

	fmt.Println()
	return nil
}

func newAPIKeysCreateCmd() *cobra.Command {
	var name string
	var scopes []string
	var expiresInDays int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		Long: `Create a new API key for programmatic access.

The full key secret is shown only once at creation. Save it immediately —
it cannot be retrieved again.`,
		Example: `  ff api-keys create --name ci-deploy
  ff api-keys create --name ci-deploy --scopes read,write
  ff api-keys create --name deploy --expires 90 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIKeysCreate(name, scopes, expiresInDays, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key name (required)")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "Access scopes (comma-separated: read,write,admin)")
	cmd.Flags().IntVar(&expiresInDays, "expires", 0, "Expiration in days (0 = no expiry)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runAPIKeysCreate(name string, scopes []string, expiresInDays int, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"name": name,
	}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	if expiresInDays > 0 {
		body["expires_in_days"] = expiresInDays
	}

	var result APIKeyCreated
	if err := client.Post("/v1/api-keys", body, &result); err != nil {
		return fmt.Errorf("could not create API key: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}

	fmt.Printf("✅ Created API key %q\n\n", name)
	fmt.Printf("  ID:     %s\n", result.ID)
	if result.KeyPrefix != "" {
		fmt.Printf("  Prefix: %s...\n", result.KeyPrefix)
	}
	if len(result.Scopes) > 0 {
		fmt.Printf("  Scopes: %s\n", strings.Join(result.Scopes, ", "))
	}
	if result.ExpiresAt != "" {
		fmt.Printf("  Expires: %s\n", result.ExpiresAt)
	}
	if result.Key != "" {
		fmt.Printf("\n  ⚠️  Key (shown once): %s\n", result.Key)
		fmt.Printf("  Save this key now — it cannot be retrieved again.\n")
	}
	return nil
}

func newAPIKeysRotateCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rotate <id>",
		Short: "Rotate an API key",
		Long: `Rotate an API key to generate a new secret. The old key is
immediately invalidated. The new secret is shown only once.`,
		Example: `  ff api-keys rotate <id>
  ff api-keys rotate <id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIKeysRotate(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAPIKeysRotate(id string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}

	if !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Rotate API key %s? The old key will be immediately invalidated.", id),
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

	var result APIKeyCreated
	if err := client.Post("/v1/api-keys/"+id+"/rotate", nil, &result); err != nil {
		return fmt.Errorf("could not rotate API key: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}

	fmt.Printf("✅ Rotated API key %s\n\n", id)
	if result.KeyPrefix != "" {
		fmt.Printf("  Prefix: %s...\n", result.KeyPrefix)
	}
	if result.Key != "" {
		fmt.Printf("  ⚠️  New key (shown once): %s\n", result.Key)
		fmt.Printf("  Save this key now — it cannot be retrieved again.\n")
	}
	return nil
}

func newAPIKeysRevokeCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "revoke <id>",
		Aliases: []string{"rm", "delete"},
		Short:   "Revoke an API key",
		Long: `Revoke an API key. The key is immediately invalidated and cannot
be restored. This action requires confirmation.`,
		Example: `  ff api-keys revoke <id>
  ff api-keys revoke <id> --force
  ff api-keys revoke <id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIKeysRevoke(args[0], force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAPIKeysRevoke(id string, force, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Revoke API key %s? This cannot be undone.", id),
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

	if err := client.Delete("/v1/api-keys/"+id, nil); err != nil {
		return fmt.Errorf("could not revoke API key: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{"success": true, "id": id, "revoked": true})
		return nil
	}

	fmt.Printf("✅ Revoked API key %s\n", id)
	return nil
}
