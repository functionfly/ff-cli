package commands

import "github.com/spf13/cobra"

func NewVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage the encrypted secrets vault",
		Long: `Manage secrets, tokens, versions, audit logs, namespaces, and more
through the FunctionFly encrypted vault.

The vault provides zero-knowledge, client-side encrypted secrets management
with enterprise features like RBAC, MFA, dynamic credentials, and audit logging.`,
		Example: `  ff vault secrets list
  ff vault secrets create --name my-key --type api_key
  ff vault tokens create --secret <id>
  ff vault versions list --secret <id>
  ff vault audit list
  ff vault namespaces list
  ff vault dynamic targets list`,
	}
	cmd.AddCommand(
		NewVaultSecretsCmd(),
		NewVaultTokensCmd(),
		NewVaultVersionsCmd(),
		NewVaultAuditCmd(),
		NewVaultNamespacesCmd(),
		NewVaultDynamicCmd(),
		NewVaultSharesCmd(),
		NewVaultRBACCmd(),
		NewVaultConfigCmd(),
	)
	return cmd
}
