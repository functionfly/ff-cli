package commands

import (
	"github.com/spf13/cobra"
)

var apiKeysCmd = &cobra.Command{
	Use:     "api-keys",
	Aliases: []string{"apikeys", "api_key"},
	Short:   "Manage API keys",
	Long: `Manage API keys for programmatic access and CI/CD pipelines.

API keys let you authenticate without a browser session. Each key has a
name, optional scopes, and an expiration. The key secret is shown only
once at creation — save it immediately.`,
	Example: `  ff api-keys list
  ff api-keys create --name ci-deploy --scopes read,write
  ff api-keys rotate <id>
  ff api-keys revoke <id>`,
	SilenceUsage: true,
}

func init() {
	apiKeysCmd.AddCommand(newAPIKeysListCmd())
	apiKeysCmd.AddCommand(newAPIKeysCreateCmd())
	apiKeysCmd.AddCommand(newAPIKeysRotateCmd())
	apiKeysCmd.AddCommand(newAPIKeysRevokeCmd())
}

func APIKeysCmd() *cobra.Command {
	return apiKeysCmd
}
