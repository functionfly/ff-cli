/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"github.com/spf13/cobra"
)

// backendCmd represents the backend command
var backendCmd = &cobra.Command{
	Use:   "backend",
	Short: "Manage execution backends",
	Long: `Manage execution backends for your applications.

Backends are the runtime environments where your functions execute.
You can add, list, and remove backends for your apps.

Examples:
  # List all backends for an app
  ff backend list --app myapp

  # Add a new backend
  ff backend add --app myapp --provider vercel --region us-east-1

  # Remove a backend
  ff backend remove --app myapp --backend <backend-id>`,
	SilenceUsage: true,
}

func init() {
	backendCmd.AddCommand(newBackendAddCmd())
	backendCmd.AddCommand(newBackendListCmd())
	backendCmd.AddCommand(newBackendRemoveCmd())
}

// BackendCmd returns the backend command for attachment to the live root (e.g. from main).
func BackendCmd() *cobra.Command {
	return backendCmd
}
