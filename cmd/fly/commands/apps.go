package commands

import (
	"github.com/spf13/cobra"
)

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage applications",
	Long: `Manage your FunctionFly applications.

Create, list, inspect, update, and delete apps. Apps are the top-level
container for backends and function routing.`,
	Example: `  ff apps list
  ff apps create my-app
  ff apps get <id>
  ff apps delete <id>`,
	SilenceUsage: true,
}

func init() {
	appsCmd.AddCommand(newAppsListCmd())
	appsCmd.AddCommand(newAppsCreateCmd())
	appsCmd.AddCommand(newAppsGetCmd())
	appsCmd.AddCommand(newAppsUpdateCmd())
	appsCmd.AddCommand(newAppsDeleteCmd())
}

func AppsCmd() *cobra.Command {
	return appsCmd
}
