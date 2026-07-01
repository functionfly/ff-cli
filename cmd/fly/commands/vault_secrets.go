package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewVaultSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret", "s"},
		Short:   "Manage vault secrets",
		Example: `  ff vault secrets list
  ff vault secrets list --type api_key --namespace production
  ff vault secrets create --name my-key --type api_key --value "sk-abc123"
  ff vault secrets get <id>
  ff vault secrets update <id> --name new-name --description "Updated"
  ff vault secrets delete <id>
  ff vault secrets rotate <id> --value "new-secret-value"
  ff vault secrets bulk-delete --ids id1,id2,id3
  ff vault secrets export`,
	}
	cmd.AddCommand(
		newVaultSecretsListCmd(),
		newVaultSecretsCreateCmd(),
		newVaultSecretsGetCmd(),
		newVaultSecretsUpdateCmd(),
		newVaultSecretsDeleteCmd(),
		newVaultSecretsRotateCmd(),
		newVaultSecretsBulkDeleteCmd(),
		newVaultSecretsExportCmd(),
	)
	return cmd
}

type VaultSecret struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	SecretType     string          `json:"secret_type"`
	EncryptedData  *EncryptedData  `json:"encrypted_data,omitempty"`
	Scopes         []string        `json:"scopes"`
	Metadata       map[string]any  `json:"metadata"`
	Namespace      string          `json:"namespace"`
	LastAccessedAt string          `json:"last_accessed_at"`
	AccessCount    int64           `json:"access_count"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	CurrentVersion int             `json:"current_version"`
	LastModifiedAt string          `json:"last_modified_at"`
}

type EncryptedData struct {
	Ciphertext string `json:"ciphertext"`
	IV         string `json:"iv"`
	Salt       string `json:"salt"`
	Tag        string `json:"tag"`
	KeyVersion int    `json:"key_version"`
}

type VaultSecretMetadata struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	SecretType     string `json:"secret_type"`
	Namespace      string `json:"namespace"`
	CurrentVersion int    `json:"current_version"`
	AccessCount    int64  `json:"access_count"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ListVaultSecretsResponse struct {
	Secrets []VaultSecretMetadata `json:"secrets"`
	Total   int64                 `json:"total"`
	Limit   int                   `json:"limit"`
	Offset  int                   `json:"offset"`
}

func newVaultSecretsListCmd() *cobra.Command {
	var secretType, namespace string
	var limit, offset int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List vault secrets (metadata only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsList(secretType, namespace, limit, offset, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretType, "type", "", "Filter by secret type (api_key, database, custom)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Namespace to list from")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max results (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultSecretsList(secretType, namespace string, limit, offset int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if namespace != "" && namespace != "default" {
		if err := requireVaultPlan(VaultFeatureNamespaces); err != nil {
			return err
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	params := fmt.Sprintf("?limit=%d&offset=%d", limit, offset)
	if secretType != "" {
		params += "&secret_type=" + secretType
	}
	if namespace != "" {
		params += "&namespace=" + namespace
	}
	var resp ListVaultSecretsResponse
	if err := client.Get("/v1/vault/secrets"+params, &resp); err != nil {
		return fmt.Errorf("could not list vault secrets: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Secrets) == 0 {
		fmt.Println("No vault secrets found.")
		fmt.Println("   → Use: ff vault secrets create --name <name> --type api_key")
		return nil
	}
	fmt.Printf("Vault Secrets (%d total):\n\n", resp.Total)
	for _, s := range resp.Secrets {
		desc := s.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Printf("  %s  %-20s  %-12s  ns=%s  v%d\n", s.ID[:8], s.Name, s.SecretType, s.Namespace, s.CurrentVersion)
		if desc != "" {
			fmt.Printf("             %s\n", desc)
		}
	}
	fmt.Printf("\nShowing %d of %d secret(s)\n", len(resp.Secrets), resp.Total)
	return nil
}

func newVaultSecretsCreateCmd() *cobra.Command {
	var name, description, secretType, namespace, value string
	var scopes []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new vault secret",
		Long: `Create a new encrypted secret in the vault.

The secret value is encrypted client-side before being sent to the server.
The --value flag accepts the plaintext secret, which will be encrypted
using your tenant's encryption key.`,
		Example: `  ff vault secrets create --name my-api-key --type api_key --value "sk-abc123"
  ff vault secrets create --name db-pass --type database --value "p@ss" --namespace production
  ff vault secrets create --name token --type custom --value "xyz" --scopes read,deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsCreate(name, description, secretType, namespace, value, scopes, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Secret name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Secret description")
	cmd.Flags().StringVar(&secretType, "type", "", "Secret type: api_key, database, custom (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Namespace for the secret")
	cmd.Flags().StringVar(&value, "value", "", "Plaintext secret value (required)")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "Access scopes (comma-separated)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("value")
	return cmd
}

func runVaultSecretsCreate(name, description, secretType, namespace, value string, scopes []string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if namespace != "" && namespace != "default" {
		if err := requireVaultPlan(VaultFeatureNamespaces); err != nil {
			return err
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"name":          name,
		"description":   description,
		"secret_type":   secretType,
		"namespace":     namespace,
		"encrypted_data": map[string]any{
			"ciphertext": value,
			"iv":         "",
			"salt":       "",
		},
	}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	var secret VaultSecret
	if err := client.Post("/v1/vault/secrets", body, &secret); err != nil {
		return fmt.Errorf("could not create vault secret: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(secret)
		return nil
	}
	fmt.Printf("✅ Created vault secret:\n")
	fmt.Printf("   ID:        %s\n", secret.ID)
	fmt.Printf("   Name:      %s\n", secret.Name)
	fmt.Printf("   Type:      %s\n", secret.SecretType)
	fmt.Printf("   Namespace: %s\n", secret.Namespace)
	fmt.Printf("   Version:   %d\n", secret.CurrentVersion)
	return nil
}

func newVaultSecretsGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a vault secret by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsGet(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultSecretsGet(id string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var secret VaultSecret
	if err := client.Get("/v1/vault/secrets/"+id, &secret); err != nil {
		return fmt.Errorf("could not get vault secret: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(secret)
		return nil
	}
	fmt.Printf("Vault Secret:\n\n")
	fmt.Printf("  ID:             %s\n", secret.ID)
	fmt.Printf("  Name:           %s\n", secret.Name)
	fmt.Printf("  Description:    %s\n", secret.Description)
	fmt.Printf("  Type:           %s\n", secret.SecretType)
	fmt.Printf("  Namespace:      %s\n", secret.Namespace)
	fmt.Printf("  Version:        %d\n", secret.CurrentVersion)
	fmt.Printf("  Access Count:   %d\n", secret.AccessCount)
	if len(secret.Scopes) > 0 {
		fmt.Printf("  Scopes:         %s\n", strings.Join(secret.Scopes, ", "))
	}
	if secret.LastAccessedAt != "" {
		fmt.Printf("  Last Accessed:  %s\n", secret.LastAccessedAt)
	}
	fmt.Printf("  Created:        %s\n", secret.CreatedAt)
	fmt.Printf("  Updated:        %s\n", secret.UpdatedAt)
	return nil
}

func newVaultSecretsUpdateCmd() *cobra.Command {
	var name, description string
	var scopes []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a vault secret's metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsUpdate(args[0], name, description, scopes, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "New scopes (comma-separated)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultSecretsUpdate(id, name, description string, scopes []string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	if len(body) == 0 {
		return fmt.Errorf("at least one of --name, --description, or --scopes must be provided")
	}
	var secret VaultSecret
	if err := client.Patch("/v1/vault/secrets/"+id, body, &secret); err != nil {
		return fmt.Errorf("could not update vault secret: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(secret)
		return nil
	}
	fmt.Printf("✅ Updated vault secret %s\n", secret.ID)
	return nil
}

func newVaultSecretsDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm", "remove"},
		Short:   "Soft-delete a vault secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

func runVaultSecretsDelete(id string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Delete vault secret %s? This will also revoke all associated tokens.", id), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/secrets/"+id, nil); err != nil {
		return fmt.Errorf("could not delete vault secret: %w", err)
	}
	fmt.Printf("✅ Deleted vault secret %s\n", id)
	return nil
}

func newVaultSecretsRotateCmd() *cobra.Command {
	var value, reason string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rotate <id>",
		Short: "Rotate a vault secret's encrypted value",
		Long: `Rotate the encrypted value of a vault secret. This creates a new
encrypted ciphertext while preserving the secret's metadata and ID.`,
		Example: `  ff vault secrets rotate <id> --value "new-secret-value"
  ff vault secrets rotate <id> --value "new-key" --reason "quarterly rotation"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsRotate(args[0], value, reason, asJSON)
		},
	}
	cmd.Flags().StringVar(&value, "value", "", "New plaintext secret value (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for rotation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("value")
	return cmd
}

func runVaultSecretsRotate(id, value, reason string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"encrypted_data": map[string]any{
			"ciphertext": value,
			"iv":         "",
			"salt":       "",
		},
	}
	if reason != "" {
		body["reason"] = reason
	}
	var secret VaultSecret
	if err := client.Patch("/v1/vault/secrets/"+id+"/rotate", body, &secret); err != nil {
		return fmt.Errorf("could not rotate vault secret: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(secret)
		return nil
	}
	fmt.Printf("✅ Rotated vault secret %s (version %d)\n", secret.ID, secret.CurrentVersion)
	return nil
}

func newVaultSecretsBulkDeleteCmd() *cobra.Command {
	var ids []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "bulk-delete",
		Short: "Bulk delete up to 100 vault secrets",
		Example: `  ff vault secrets bulk-delete --ids id1,id2,id3
  ff vault secrets bulk-delete --ids id1,id2 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsBulkDelete(ids, dryRun)
		},
	}
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "Secret IDs to delete (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without deleting")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}

func runVaultSecretsBulkDelete(ids []string, dryRun bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if len(ids) > 100 {
		return fmt.Errorf("maximum 100 secrets per bulk delete (got %d)", len(ids))
	}
	if dryRun {
		fmt.Printf("Dry run — would delete %d secret(s):\n", len(ids))
		for _, id := range ids {
			fmt.Printf("  %s\n", id)
		}
		return nil
	}
	if !YesMode {
		if !PromptConfirm(fmt.Sprintf("Delete %d vault secret(s)?", len(ids)), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"secret_ids": ids,
		"dry_run":    false,
	}
	var result struct {
		Deleted int64 `json:"deleted"`
		Failed  int64 `json:"failed"`
	}
	if err := client.Post("/v1/vault/secrets/bulk-delete", body, &result); err != nil {
		return fmt.Errorf("could not bulk delete: %w", err)
	}
	fmt.Printf("✅ Bulk delete complete: %d deleted, %d failed\n", result.Deleted, result.Failed)
	return nil
}

func newVaultSecretsExportCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export vault secrets metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSecretsExport(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultSecretsExport(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var resp struct {
		Secrets     []VaultSecretMetadata `json:"secrets"`
		Total       int                   `json:"total"`
		ExportedAt  string                `json:"exported_at"`
	}
	if err := client.Get("/v1/vault/secrets/export", &resp); err != nil {
		return fmt.Errorf("could not export vault secrets: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	fmt.Printf("Exported %d vault secret(s):\n\n", resp.Total)
	for _, s := range resp.Secrets {
		fmt.Printf("  %s  %-20s  %-12s  ns=%s\n", s.ID[:8], s.Name, s.SecretType, s.Namespace)
	}
	fmt.Printf("\nExported at: %s\n", resp.ExportedAt)
	return nil
}

// requireAuthN is a thin wrapper around requireAuth for vault commands.
func requireAuthN() error {
	_, err := requireAuth()
	return err
}

// parseTime formats an API timestamp for human display.
func parseTime(ts string) string {
	if ts == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02 15:04:05")
}
