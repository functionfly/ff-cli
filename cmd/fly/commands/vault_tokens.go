package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tokens",
		Aliases: []string{"token"},
		Short:   "Manage vault access tokens",
		Example: `  ff vault tokens create --secret <id> --name ci-token --scopes read,write
  ff vault tokens list --secret <id>
  ff vault tokens revoke <token-id>`,
	}
	cmd.AddCommand(
		newVaultTokensCreateCmd(),
		newVaultTokensListCmd(),
		newVaultTokensRevokeCmd(),
	)
	return cmd
}

type VaultToken struct {
	TokenID   string   `json:"token_id"`
	Token     string   `json:"token,omitempty"`
	SecretID  string   `json:"secret_id"`
	Name      string   `json:"name"`
	ExpiresAt string   `json:"expires_at"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
}

type VaultTokenListResponse struct {
	Tokens []VaultToken `json:"tokens"`
	Total  int64        `json:"total"`
}

func newVaultTokensCreateCmd() *cobra.Command {
	var secretID, name string
	var scopes []string
	var expiresInHours int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate an access token for a vault secret",
		Long: `Generate a new access token for a vault secret. The token plaintext
is shown only once — save it immediately.`,
		Example: `  ff vault tokens create --secret <secret-id> --name ci-token
  ff vault tokens create --secret <id> --name deploy --scopes read,write --expires 720`,
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultTokensCreate(secretID, name, scopes, expiresInHours, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Token name")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "Access scopes (comma-separated)")
	cmd.Flags().IntVar(&expiresInHours, "expires", 24, "Expiration in hours (max 8760)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	return cmd
}

func runVaultTokensCreate(secretID, name string, scopes []string, expiresInHours int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if expiresInHours < 1 || expiresInHours > 8760 {
		return fmt.Errorf("expires must be between 1 and 8760 hours")
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"expires_in_hours": expiresInHours,
	}
	if name != "" {
		body["name"] = name
	}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	var token VaultToken
	if err := client.Post("/v1/vault/secrets/"+secretID+"/tokens", body, &token); err != nil {
		return fmt.Errorf("could not create token: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(token)
		return nil
	}
	fmt.Printf("✅ Created access token:\n\n")
	fmt.Printf("  Token ID:   %s\n", token.TokenID)
	fmt.Printf("  Name:       %s\n", token.Name)
	fmt.Printf("  Secret ID:  %s\n", token.SecretID)
	fmt.Printf("  Expires:    %s\n", token.ExpiresAt)
	if token.Token != "" {
		fmt.Printf("\n  ⚠️  Token (shown once): %s\n", token.Token)
		fmt.Printf("  Save this token now — it cannot be retrieved again.\n")
	}
	return nil
}

func newVaultTokensListCmd() *cobra.Command {
	var secretID string
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List access tokens for a vault secret",
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultTokensList(secretID, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	return cmd
}

func runVaultTokensList(secretID string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var resp VaultTokenListResponse
	if err := client.Get("/v1/vault/secrets/"+secretID+"/tokens", &resp); err != nil {
		return fmt.Errorf("could not list tokens: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Tokens) == 0 {
		fmt.Println("No tokens found for this secret.")
		fmt.Println("   → Use: ff vault tokens create --secret <id>")
		return nil
	}
	fmt.Printf("Access Tokens for secret %s (%d total):\n\n", secretID, resp.Total)
	for _, t := range resp.Tokens {
		fmt.Printf("  %s  %-20s  expires=%s\n", t.TokenID[:8], t.Name, parseTime(t.ExpiresAt))
	}
	return nil
}

func newVaultTokensRevokeCmd() *cobra.Command {
	var reason string
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "revoke <token-id>",
		Aliases: []string{"rm"},
		Short:   "Revoke an access token",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultTokensRevoke(args[0], reason, asJSON)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for revocation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultTokensRevoke(tokenID, reason string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var result struct {
		Message  string `json:"message"`
		TokenID  string `json:"token_id"`
		Revoked  bool   `json:"revoked"`
	}
	if err := client.Delete("/v1/vault/tokens/"+tokenID, &result); err != nil {
		return fmt.Errorf("could not revoke token: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Revoked token %s\n", tokenID)
	return nil
}
