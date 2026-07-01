package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type AppInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type AppsListResponse struct {
	Apps  []AppInfo `json:"apps"`
	Total int       `json:"total,omitempty"`
}

func newAppsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all apps",
		Long:  `List all applications for the current user or tenant.`,
		Example: `  ff apps list
  ff apps list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAppsList(asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var resp AppsListResponse
	if err := client.Get("/v1/apps", &resp); err != nil {
		return fmt.Errorf("could not list apps: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Apps) == 0 {
		fmt.Println("No apps found.")
		fmt.Println("\nCreate your first app with: ff apps create <name>")
		return nil
	}

	fmt.Printf("\nApps (%d)\n\n", len(resp.Apps))
	fmt.Printf("  %-36s  %-24s  %s\n", "ID", "NAME", "UPDATED")
	fmt.Println("  " + strings.Repeat("-", 80))

	for _, app := range resp.Apps {
		updatedAt := app.UpdatedAt
		if len(updatedAt) > 10 {
			updatedAt = updatedAt[:10]
		}
		fmt.Printf("  %-36s  %-24s  %s\n", app.ID, app.Name, updatedAt)
	}

	fmt.Println()
	return nil
}

func newAppsCreateCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new app",
		Long: `Create a new application. The app name must be unique within your
tenant and will be used as the slug for API routing.`,
		Example: `  ff apps create my-app
  ff apps create my-app --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsCreate(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAppsCreate(name string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{"name": name}
	var app AppInfo
	if err := client.Post("/v1/apps", body, &app); err != nil {
		return fmt.Errorf("could not create app: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(app)
		return nil
	}

	fmt.Printf("✅ Created app %s\n", app.Name)
	fmt.Printf("   ID:   %s\n", app.ID)
	if app.Slug != "" {
		fmt.Printf("   Slug: %s\n", app.Slug)
	}
	return nil
}

func newAppsGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get app details",
		Long:  `Get detailed information about an application by its ID.`,
		Example: `  ff apps get <id>
  ff apps get <id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsGet(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAppsGet(id string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var app AppInfo
	if err := client.Get("/v1/apps/"+id, &app); err != nil {
		return fmt.Errorf("could not get app: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(app)
		return nil
	}

	fmt.Printf("App: %s\n\n", app.Name)
	fmt.Printf("  ID:         %s\n", app.ID)
	if app.Slug != "" {
		fmt.Printf("  Slug:       %s\n", app.Slug)
	}
	if app.CreatedAt != "" {
		fmt.Printf("  Created:    %s\n", app.CreatedAt)
	}
	if app.UpdatedAt != "" {
		fmt.Printf("  Updated:    %s\n", app.UpdatedAt)
	}
	return nil
}

func newAppsUpdateCmd() *cobra.Command {
	var asJSON bool
	var newName string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an app",
		Long:  `Update an application's properties.`,
		Example: `  ff apps update <id> --name new-name
  ff apps update <id> --name new-name --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsUpdate(args[0], newName, asJSON)
		},
	}
	cmd.Flags().StringVar(&newName, "name", "", "New app name")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runAppsUpdate(id, newName string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{"name": newName}
	var app AppInfo
	if err := client.Patch("/v1/apps/"+id, body, &app); err != nil {
		return fmt.Errorf("could not update app: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(app)
		return nil
	}

	fmt.Printf("✅ Updated app %s\n", app.ID)
	if app.Name != "" {
		fmt.Printf("   Name: %s\n", app.Name)
	}
	return nil
}

func newAppsDeleteCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an app",
		Long: `Delete an application and all its associated backends.

This action is permanent and requires confirmation unless --force or --yes
is passed.`,
		Example: `  ff apps delete <id>
  ff apps delete <id> --force
  ff apps delete <id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsDelete(args[0], force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runAppsDelete(id string, force, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Delete app %s? This permanently removes the app and all its backends.", id),
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

	if err := client.Delete("/v1/apps/"+id, nil); err != nil {
		return fmt.Errorf("could not delete app: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{"success": true, "id": id, "deleted": true})
		return nil
	}

	fmt.Printf("✅ Deleted app %s\n", id)
	return nil
}
