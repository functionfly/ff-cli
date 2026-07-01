package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultSharesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shares",
		Aliases: []string{"share"},
		Short:   "Manage cross-tenant secret sharing",
		Example: `  ff vault shares create --secret <id> --grantee <tenant-id> --permissions read
  ff vault shares list
  ff vault shares revoke <share-id>`,
	}
	cmd.AddCommand(
		newVaultSharesCreateCmd(),
		newVaultSharesListCmd(),
		newVaultSharesRevokeCmd(),
	)
	return cmd
}

type ShareResponse struct {
	ID              string `json:"id"`
	SecretID        string `json:"secret_id"`
	GranteeTenantID string `json:"grantee_tenant_id"`
	Permissions     string `json:"permissions"`
	ExpiresAt       string `json:"expires_at"`
	CreatedAt       string `json:"created_at"`
}

func newVaultSharesCreateCmd() *cobra.Command {
	var secretID, granteeTenantID, permissions, expiresAt string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Share a vault secret with another tenant",
		Example: `  ff vault shares create --secret <id> --grantee <tenant-id> --permissions read
  ff vault shares create --secret <id> --grantee <tenant-id> --permissions read-write --expires 2025-12-31T23:59:59Z`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSharesCreate(secretID, granteeTenantID, permissions, expiresAt, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().StringVar(&granteeTenantID, "grantee", "", "Grantee tenant ID (required)")
	cmd.Flags().StringVar(&permissions, "permissions", "read", "Permissions: read or read-write")
	cmd.Flags().StringVar(&expiresAt, "expires", "", "Expiration (RFC3339)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	_ = cmd.MarkFlagRequired("grantee")
	return cmd
}

func runVaultSharesCreate(secretID, granteeTenantID, permissions, expiresAt string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureShares); err != nil {
		return err
	}
	if permissions != "read" && permissions != "read-write" {
		return fmt.Errorf("permissions must be 'read' or 'read-write'")
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"grantee_tenant_id": granteeTenantID,
		"permissions":       permissions,
	}
	if expiresAt != "" {
		body["expires_at"] = expiresAt
	}
	var share ShareResponse
	if err := client.Post("/v1/vault/secrets/"+secretID+"/share", body, &share); err != nil {
		return fmt.Errorf("could not create share: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(share)
		return nil
	}
	fmt.Printf("✅ Created secret share:\n")
	fmt.Printf("   Share ID:  %s\n", share.ID)
	fmt.Printf("   Secret:    %s\n", share.SecretID)
	fmt.Printf("   Grantee:   %s\n", share.GranteeTenantID)
	fmt.Printf("   Perms:     %s\n", share.Permissions)
	return nil
}

func newVaultSharesListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List secrets shared with your tenant",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSharesList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultSharesList(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureShares); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var shares []ShareResponse
	if err := client.Get("/v1/vault/shared", &shares); err != nil {
		return fmt.Errorf("could not list shares: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(shares)
		return nil
	}
	if len(shares) == 0 {
		fmt.Println("No secrets shared with your tenant.")
		return nil
	}
	fmt.Printf("Shared Secrets (%d):\n\n", len(shares))
	for _, s := range shares {
		fmt.Printf("  %s  secret=%s  grantee=%s  perms=%s\n", s.ID[:8], s.SecretID[:8], s.GranteeTenantID[:8], s.Permissions)
	}
	return nil
}

func newVaultSharesRevokeCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "revoke <share-id>",
		Aliases: []string{"rm"},
		Short:   "Revoke a secret share",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultSharesRevoke(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runVaultSharesRevoke(shareID string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureShares); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Revoke share %s?", shareID), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/shares/"+shareID, nil); err != nil {
		return fmt.Errorf("could not revoke share: %w", err)
	}
	fmt.Printf("✅ Revoked share %s\n", shareID)
	return nil
}
