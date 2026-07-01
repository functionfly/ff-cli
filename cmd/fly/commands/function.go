package commands

import (
	"github.com/spf13/cobra"
)

var functionCmd = &cobra.Command{
	Use:     "function",
	Aliases: []string{"fn", "func"},
	Short:   "Inspect function details",
	Long: `View detailed information about a deployed function.

Shows a single aggregated view including metadata, versions, stats,
trust score, and reviews.`,
	Example: `  ff function info
  ff function info alice/my-fn
  ff function info alice/my-fn --json`,
	SilenceUsage: true,
}

func init() {
	functionCmd.AddCommand(newFunctionInfoCmd())
}

func FunctionCmd() *cobra.Command {
	return functionCmd
}
