package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewDeleteCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "delete [author/name]",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a deployed function",
		Long: `Delete a function from the FunctionFly registry.

This permanently removes the function and all its versions. The action
requires confirmation unless --force or --yes is passed.`,
		Example: "  ff delete\n  ff delete alice/my-fn\n  ff rm alice/my-fn --force\n  ff delete --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(args, force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDelete(args []string, force, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Delete %s/%s? This permanently removes all versions.", author, name),
			false,
		)
		if !confirmed {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/functions/%s/%s", author, name)
	if err := client.Delete(path, nil); err != nil {
		return fmt.Errorf("could not delete function: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"success":  true,
			"author":   author,
			"name":     name,
			"deleted":  true,
		})
		return nil
	}

	fmt.Printf("✅ Deleted %s/%s\n", author, name)
	return nil
}
