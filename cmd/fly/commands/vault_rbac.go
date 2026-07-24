package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultRBACCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rbac",
		Short: "Manage vault RBAC (roles and assignments)",
		Long: `Manage role-based access control for the vault.
Create custom roles, assign roles to users, and view your own assignments.`,
		Example: `  ff vault rbac roles list
  ff vault rbac roles create --name deployer --description "Can deploy secrets"
  ff vault rbac assignments list
  ff vault rbac assign --role <role-id> --user <user-id>
  ff vault rbac unassign <assignment-id>`,
	}
	cmd.AddCommand(
		NewVaultRBACRolesCmd(),
		newVaultRBACAssignmentsCmd(),
		newVaultRBACAssignCmd(),
		newVaultRBACUnassignCmd(),
	)
	return cmd
}

// ── Roles ───────────────────────────────────────────────────────────────────

func NewVaultRBACRolesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "roles",
		Aliases: []string{"role"},
		Short:   "Manage vault roles",
		Example: `  ff vault rbac roles list
  ff vault rbac roles create --name deployer --description "Deploy access"
  ff vault rbac roles update <id> --name new-name
  ff vault rbac roles delete <id>`,
	}
	cmd.AddCommand(
		newRBACRolesListCmd(),
		newRBACRolesCreateCmd(),
		newRBACRolesUpdateCmd(),
		newRBACRolesDeleteCmd(),
	)
	return cmd
}

type VaultRole struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Permissions map[string]any `json:"permissions"`
	Builtin     bool           `json:"builtin"`
	CreatedAt   string         `json:"created_at"`
}

type RoleAssignment struct {
	ID        string `json:"id"`
	RoleID    string `json:"role_id"`
	RoleName  string `json:"role_name"`
	UserID    string `json:"user_id"`
	Scope     string `json:"scope"`
	CreatedAt string `json:"created_at"`
}

func newRBACRolesListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all vault roles",
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACRolesList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runRBACRolesList(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var roles []VaultRole
	if err := client.Get("/v1/vault/roles", &roles); err != nil {
		return fmt.Errorf("could not list roles: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(roles)
		return nil
	}
	if len(roles) == 0 {
		fmt.Println("No roles found.")
		return nil
	}
	fmt.Printf("Vault Roles (%d):\n\n", len(roles))
	for _, r := range roles {
		builtin := ""
		if r.Builtin {
			builtin = " [built-in]"
		}
		fmt.Printf("  %s  %-20s%s\n", r.ID[:8], r.Name, builtin)
		if r.Description != "" {
			fmt.Printf("             %s\n", r.Description)
		}
	}
	return nil
}

func newRBACRolesCreateCmd() *cobra.Command {
	var name, description string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a custom vault role",
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACRolesCreate(name, description, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Role description")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runRBACRolesCreate(name, description string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"name": name,
	}
	if description != "" {
		body["description"] = description
	}
	var role VaultRole
	if err := client.Post("/v1/vault/roles", body, &role); err != nil {
		return fmt.Errorf("could not create role: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(role)
		return nil
	}
	fmt.Printf("✅ Created role:\n")
	fmt.Printf("   ID:   %s\n", role.ID)
	fmt.Printf("   Name: %s\n", role.Name)
	return nil
}

func newRBACRolesUpdateCmd() *cobra.Command {
	var name, description string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a non-builtin vault role",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACRolesUpdate(args[0], name, description, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runRBACRolesUpdate(id, name, description string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	if len(body) == 0 {
		return fmt.Errorf("at least one of --name or --description must be provided")
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var role VaultRole
	if err := client.Patch("/v1/vault/roles/"+id, body, &role); err != nil {
		return fmt.Errorf("could not update role: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(role)
		return nil
	}
	fmt.Printf("✅ Updated role %s\n", role.ID)
	return nil
}

func newRBACRolesDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a non-builtin vault role",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACRolesDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runRBACRolesDelete(id string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Delete role %s?", id), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/roles/"+id, nil); err != nil {
		return fmt.Errorf("could not delete role: %w", err)
	}
	fmt.Printf("✅ Deleted role %s\n", id)
	return nil
}

// ── Assignments ─────────────────────────────────────────────────────────────

func newVaultRBACAssignmentsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "assignments",
		Aliases: []string{"assigns"},
		Short:   "List your current role assignments",
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACAssignmentsList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runRBACAssignmentsList(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var assignments []RoleAssignment
	if err := client.Get("/v1/vault/my-assignments", &assignments); err != nil {
		return fmt.Errorf("could not list assignments: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(assignments)
		return nil
	}
	if len(assignments) == 0 {
		fmt.Println("No role assignments found.")
		return nil
	}
	fmt.Printf("Your Role Assignments (%d):\n\n", len(assignments))
	for _, a := range assignments {
		fmt.Printf("  %s  role=%-20s  scope=%s\n", a.ID[:8], a.RoleName, a.Scope)
	}
	return nil
}

func newVaultRBACAssignCmd() *cobra.Command {
	var roleID, userID, scope string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "assign",
		Short: "Assign a vault role to a user",
		Example: `  ff vault rbac assign --role <role-id> --user <user-id>
  ff vault rbac assign --role <role-id> --user <user-id> --scope production`,
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACAssign(roleID, userID, scope, asJSON)
		},
	}
	cmd.Flags().StringVar(&roleID, "role", "", "Role ID (required)")
	cmd.Flags().StringVar(&userID, "user", "", "User ID (required)")
	cmd.Flags().StringVar(&scope, "scope", "all", "Assignment scope")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("role")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func runRBACAssign(roleID, userID, scope string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"user_id": userID,
		"scope":   scope,
	}
	var assignment RoleAssignment
	if err := client.Post("/v1/vault/roles/"+roleID+"/assignments", body, &assignment); err != nil {
		return fmt.Errorf("could not assign role: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(assignment)
		return nil
	}
	fmt.Printf("✅ Assigned role to user:\n")
	fmt.Printf("   Assignment: %s\n", assignment.ID)
	fmt.Printf("   Role:       %s\n", assignment.RoleName)
	fmt.Printf("   User:       %s\n", assignment.UserID)
	fmt.Printf("   Scope:      %s\n", assignment.Scope)
	return nil
}

func newVaultRBACUnassignCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "unassign <assignment-id>",
		Short: "Remove a role assignment",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runRBACUnassign(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runRBACUnassign(assignmentID string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureRBAC); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Remove role assignment %s?", assignmentID), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/role-assignments/"+assignmentID, nil); err != nil {
		return fmt.Errorf("could not remove assignment: %w", err)
	}
	fmt.Printf("✅ Removed role assignment %s\n", assignmentID)
	return nil
}
