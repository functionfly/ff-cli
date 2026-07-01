package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultNamespacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "namespaces",
		Aliases: []string{"namespace", "ns"},
		Short:   "Manage vault namespaces",
		Long: `Namespaces provide hierarchical organization for secrets.
Requires Pro+ plan. Paths are lowercase, /-separated, max 5 segments.
Reserved paths: default, shared, system.`,
		Example: `  ff vault namespaces list
  ff vault namespaces create --path "production/api-keys"
  ff vault namespaces delete <id>`,
	}
	cmd.AddCommand(
		newVaultNamespacesListCmd(),
		newVaultNamespacesCreateCmd(),
		newVaultNamespacesDeleteCmd(),
	)
	return cmd
}

type VaultNamespace struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Path        string `json:"path"`
	Description string `json:"description"`
	ParentID    string `json:"parent_id"`
	CreatedAt   string `json:"created_at"`
}

func newVaultNamespacesListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List vault namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultNamespacesList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultNamespacesList(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureNamespaces); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var namespaces []VaultNamespace
	if err := client.Get("/v1/vault/namespaces", &namespaces); err != nil {
		return fmt.Errorf("could not list namespaces: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(namespaces)
		return nil
	}
	if len(namespaces) == 0 {
		fmt.Println("No namespaces found.")
		fmt.Println("   → Use: ff vault namespaces create --path \"my-namespace\"")
		return nil
	}
	fmt.Printf("Vault Namespaces (%d):\n\n", len(namespaces))
	for _, ns := range namespaces {
		fmt.Printf("  %s  %-30s", ns.ID[:8], ns.Path)
		if ns.Description != "" {
			fmt.Printf("  %s", ns.Description)
		}
		fmt.Println()
	}
	return nil
}

func newVaultNamespacesCreateCmd() *cobra.Command {
	var path, description, parentID string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a vault namespace",
		Example: `  ff vault namespaces create --path "production/api-keys"
  ff vault namespaces create --path "staging/secrets" --description "Staging environment"
  ff vault namespaces create --path "prod/db" --parent <parent-ns-id>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultNamespacesCreate(path, description, parentID, asJSON)
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "Namespace path (required, lowercase, /-separated)")
	cmd.Flags().StringVar(&description, "description", "", "Namespace description")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent namespace ID (for nesting)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func runVaultNamespacesCreate(path, description, parentID string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureNamespaces); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"path": path,
	}
	if description != "" {
		body["description"] = description
	}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	var ns VaultNamespace
	if err := client.Post("/v1/vault/namespaces", body, &ns); err != nil {
		return fmt.Errorf("could not create namespace: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(ns)
		return nil
	}
	fmt.Printf("✅ Created namespace:\n")
	fmt.Printf("   ID:   %s\n", ns.ID)
	fmt.Printf("   Path: %s\n", ns.Path)
	return nil
}

func newVaultNamespacesDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a vault namespace",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultNamespacesDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runVaultNamespacesDelete(id string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureNamespaces); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Delete namespace %s?", id), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/namespaces/"+id, nil); err != nil {
		return fmt.Errorf("could not delete namespace: %w", err)
	}
	fmt.Printf("✅ Deleted namespace %s\n", id)
	return nil
}
