/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	appName         string
	backendProvider string
	backendRegion   string
	backendURL      string
	backendSecret   string
)

// newBackendAddCmd creates the backend add command
func newBackendAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new backend",
		Long: `Add a new execution backend to your application.

A backend is a runtime environment where your functions execute.
Supported providers: vercel, cloudflare, deno, fly`,
		Example: `  # Add a Vercel backend
  ff backend add --app myapp --provider vercel --region us-east-1

  # Add a Cloudflare Workers backend
  ff backend add --app myapp --provider cloudflare --region global

  # Add a backend with custom URL
  ff backend add --app myapp --provider custom --url https://my-backend.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendAdd(cmd)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "Application name (required)")
	cmd.Flags().StringVar(&backendProvider, "provider", "", "Backend provider (vercel, cloudflare, deno, fly, custom)")
	cmd.Flags().StringVar(&backendRegion, "region", "us-east-1", "Backend region")
	cmd.Flags().StringVar(&backendURL, "url", "", "Backend URL (required for custom provider)")
	cmd.Flags().StringVar(&backendSecret, "secret", "", "Shared secret for backend authentication")

	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("provider")

	return cmd
}

func runBackendAdd(cmd *cobra.Command) error {
	// Validate provider
	validProviders := map[string]bool{
		"vercel":     true,
		"cloudflare": true,
		"deno":       true,
		"fly":        true,
		"custom":     true,
	}

	if !validProviders[backendProvider] {
		return fmt.Errorf("invalid provider: %s. Valid providers: vercel, cloudflare, deno, fly, custom", backendProvider)
	}

	// For custom provider, URL is required
	if backendProvider == "custom" && backendURL == "" {
		return fmt.Errorf("--url is required for custom provider")
	}

	// Get API client
	client, err := NewAPIClient()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Get app ID from name
	app, err := client.GetApp(appName)
	if err != nil {
		return fmt.Errorf("failed to get app: %w", err)
	}

	// Create backend
	backend, err := client.CreateBackend(app.ID, backendProvider, backendRegion, backendURL, backendSecret)
	if err != nil {
		return fmt.Errorf("failed to create backend: %w", err)
	}

	fmt.Printf("Successfully created backend %s\n", backend.ID)
	fmt.Printf("  Provider: %s\n", backend.Provider)
	fmt.Printf("  Region: %s\n", backend.Region)
	fmt.Printf("  URL: %s\n", backend.URL)

	return nil
}
