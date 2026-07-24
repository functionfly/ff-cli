package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func NewCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for ff.

Bash:   source <(ff completion bash)
Zsh:    source <(ff completion zsh)
Fish:   ff completion fish | source
PS:     ff completion powershell | Out-String | Invoke-Expression`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unknown shell: %s", args[0])
			}
		},
	}
	return cmd
}

// NewCompletionsAliasCmd returns an alias for "completion" (plural form).
func NewCompletionsAliasCmd(root *cobra.Command) *cobra.Command {
	cmd := NewCompletionCmd(root)
	cmd.Use = "completions [bash|zsh|fish|powershell]"
	cmd.Short = "Alias for 'completion' (generate shell completion scripts)"
	cmd.Hidden = true
	return cmd
}
