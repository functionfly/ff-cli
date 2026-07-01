package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewListCmd() *cobra.Command {
	var asJSON bool
	var showAll bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List deployed functions",
		Long: `List all deployed functions for the current user or tenant.

By default, shows functions owned by the authenticated user. Use --all to
include public functions from other authors.`,
		Example: "  ff list\n  ff ls\n  ff list --all\n  ff list --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(asJSON, showAll)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&showAll, "all", false, "Include public functions from all authors")
	return cmd
}

type FunctionEntry struct {
	ID          string `json:"id"`
	Author      string `json:"author"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Runtime     string `json:"runtime"`
	Public      bool   `json:"public"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Description string `json:"description,omitempty"`
}

type FunctionsResponse struct {
	Functions []FunctionEntry `json:"functions"`
	Total     int             `json:"total,omitempty"`
}

func runList(asJSON, showAll bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	path := "/v1/functions"
	if showAll {
		path = "/v1/functions?all=true"
	}

	var resp FunctionsResponse
	if err := client.Get(path, &resp); err != nil {
		pathV2 := "/v2/functions"
		if showAll {
			pathV2 = "/v2/functions?all=true"
		}
		if err2 := client.Get(pathV2, &resp); err2 != nil {
			return fmt.Errorf("could not list functions: %w", err)
		}
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Functions) == 0 {
		fmt.Println("No deployed functions found.")
		fmt.Println("\nPublish your first function with: ff init <name> && ff publish")
		return nil
	}

	fmt.Printf("\nDeployed functions (%d)\n\n", len(resp.Functions))
	fmt.Printf("  %-36s  %-12s  %-10s  %-8s  %s\n", "ID", "VERSION", "RUNTIME", "VISIBILITY", "UPDATED")
	fmt.Println("  " + strings.Repeat("-", 90))

	for _, fn := range resp.Functions {
		visibility := "private"
		if fn.Public {
			visibility = "public"
		}
		displayID := fn.Author + "/" + fn.Name
		updatedAt := fn.UpdatedAt
		if len(updatedAt) > 10 {
			updatedAt = updatedAt[:10]
		}
		fmt.Printf("  %-36s  %-12s  %-10s  %-8s  %s\n",
			displayID,
			fn.Version,
			fn.Runtime,
			visibility,
			updatedAt,
		)
	}

	fmt.Println()
	return nil
}
