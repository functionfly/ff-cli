/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var backendID string

// newBackendRemoveCmd creates the backend remove command
func newBackendRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a backend",
		Long:  `Remove an execution backend from your application.`,
		Example: `  # Remove a backend by ID
  ff backend remove --app myapp --backend <backend-id>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendRemove(cmd)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "Application name (required)")
	cmd.Flags().StringVar(&backendID, "backend", "", "Backend ID to remove (required)")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("backend")

	return cmd
}

func runBackendRemove(cmd *cobra.Command) error {
	client, err := NewAPIClient()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	app, err := client.GetApp(appName)
	if err != nil {
		return fmt.Errorf("failed to get app: %w", err)
	}

	if err := client.DeleteBackend(app.ID, backendID); err != nil {
		return fmt.Errorf("failed to remove backend: %w", err)
	}

	fmt.Printf("Successfully removed backend %s\n", backendID)
	return nil
}
