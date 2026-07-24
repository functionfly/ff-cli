package commands

import (
	"github.com/spf13/cobra"
)

func NewImplantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "implant",
		Short:   "Manage FunctionFly implants (FCI artifacts)",
		Long:    "Build, sign, and publish FCI (FunctionFly Capabilty Implant) artifacts.",
		Aliases: []string{"fci"},
	}

	cmd.AddCommand(
		NewImplantInitCmd(),
		NewImplantBuildCmd(),
		NewImplantSignCmd(),
		NewImplantPublishCmd(),
		NewImplantValidateCmd(),
		NewImplantListCmd(),
		NewImplantDiffCmd(),
	)

	return cmd
}
