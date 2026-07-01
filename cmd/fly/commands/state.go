package commands

import (
	"github.com/spf13/cobra"
)

var stateCmd = &cobra.Command{
	Use:     "state",
	Aliases: []string{"kv", "store"},
	Short:   "Manage function state fabric",
	Long: `Manage persistent key-value state for your functions.

The state fabric provides per-function durable storage — a distributed
key-value store that functions can read/write at runtime. Use the CLI to
inspect, set, and manage state entries.

All operations are scoped to the function in the current directory
(or pass author/name explicitly).`,
	Example: `  ff state list
  ff state get my-key
  ff state set my-key '{"count": 42}'
  ff state set my-key "hello" --ttl 3600
  ff state delete my-key
  ff state clear --force
  ff state export --output state.json
  ff state import --file state.json`,
	SilenceUsage: true,
}

func init() {
	stateCmd.AddCommand(newStateListCmd())
	stateCmd.AddCommand(newStateGetCmd())
	stateCmd.AddCommand(newStateSetCmd())
	stateCmd.AddCommand(newStateDeleteCmd())
	stateCmd.AddCommand(newStateClearCmd())
	stateCmd.AddCommand(newStateExportCmd())
	stateCmd.AddCommand(newStateImportCmd())
}

func StateCmd() *cobra.Command {
	return stateCmd
}
