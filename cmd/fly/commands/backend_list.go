/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newBackendListCmd creates the backend list command
func newBackendListCmd() *cobra.Command {
	var asJSONList bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backends",
		Long: `List all backends for your application.

This command shows all configured execution backends for an application,
including their status, region, and health information.`,
		Example: `  # List backends for an app
  ff backend list --app myapp

  # List backends with JSON output
  ff backend list --app myapp --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendList(cmd, asJSONList)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "Application name (required)")
	cmd.Flags().BoolVar(&asJSONList, "json", false, "Output as JSON")

	_ = cmd.MarkFlagRequired("app")

	return cmd
}

func runBackendList(cmd *cobra.Command, asJSON bool) error {
	client, err := NewAPIClient()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	app, err := client.GetApp(appName)
	if err != nil {
		return fmt.Errorf("failed to get app: %w", err)
	}

	status, err := client.GetStatus(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get app status: %w", err)
	}

	if asJSON {
		printJSON(status)
		return nil
	}

	if len(status.Backends) == 0 {
		fmt.Printf("No backends configured for app %s\n", appName)
		return nil
	}

	fmt.Printf("\nBackends for %s\n\n", appName)
	fmt.Printf("%-10s %-12s %-12s %-40s %s\n", "ID", "Provider", "Region", "URL", "Status")
	fmt.Println(strings.Repeat("-", 95))

	for _, backend := range status.Backends {
		statusText := "unknown"
		if backend.CircuitState != nil {
			statusText = backend.CircuitState.State
		}
		if backend.LatestHealthCheck != nil && backend.LatestHealthCheck.OK {
			statusText = "healthy"
		}

		backendID := backend.Backend.ID
		if len(backendID) > 8 {
			backendID = backendID[:8]
		}

		url := backend.Backend.URL
		if len(url) > 38 {
			url = url[:35] + "..."
		}

		fmt.Printf("%-10s %-12s %-12s %-40s %s\n",
			backendID,
			backend.Backend.Provider,
			backend.Backend.Region,
			url,
			statusText,
		)
	}

	fmt.Println()

	return nil
}
