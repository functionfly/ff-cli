package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultDynamicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dynamic",
		Aliases: []string{"dyn"},
		Short:   "Manage dynamic secrets (database credentials, API keys)",
		Long: `Dynamic secrets are generated on-demand with automatic expiration.
Connect to database targets and generate short-lived credentials.`,
		Example: `  ff vault dynamic targets list
  ff vault dynamic targets create --name prod-db --db postgres --host db.example.com ...
  ff vault dynamic credentials list
  ff vault dynamic credentials generate <id>
  ff vault dynamic leases renew --credential <id> --lease <lease-id>`,
	}
	cmd.AddCommand(
		NewVaultDynamicTargetsCmd(),
		NewVaultDynamicCredentialsCmd(),
		NewVaultDynamicLeasesCmd(),
	)
	return cmd
}

// ── Targets ─────────────────────────────────────────────────────────────────

func NewVaultDynamicTargetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "targets",
		Aliases: []string{"target"},
		Short:   "Manage dynamic secret database targets",
		Example: `  ff vault dynamic targets list
  ff vault dynamic targets create --name prod-db --db postgres --host db.example.com --port 5432 --database mydb --user admin --password secret
  ff vault dynamic targets test <id>
  ff vault dynamic targets delete <id>`,
	}
	cmd.AddCommand(
		newDynTargetsListCmd(),
		newDynTargetsCreateCmd(),
		newDynTargetsTestCmd(),
		newDynTargetsDeleteCmd(),
	)
	return cmd
}

type DynTarget struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DBType           string   `json:"db_type"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	DatabaseName     string   `json:"database_name"`
	SSLMode          string   `json:"ssl_mode"`
	AllowedRoles     []string `json:"allowed_roles"`
	DefaultTTLSeconds int     `json:"default_ttl_seconds"`
	MaxTTLSeconds    int      `json:"max_ttl_seconds"`
	CreatedAt        string   `json:"created_at"`
}

func newDynTargetsListCmd() *cobra.Command {
	var limit, offset int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List dynamic secret targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynTargetsList(limit, offset, asJSON)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Max results (1-200)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDynTargetsList(limit, offset int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var resp struct {
		Targets []DynTarget `json:"targets"`
		Total   int         `json:"total"`
	}
	path := fmt.Sprintf("/v1/vault/dynamic-secret-targets?limit=%d&offset=%d", limit, offset)
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not list targets: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Targets) == 0 {
		fmt.Println("No dynamic secret targets found.")
		return nil
	}
	fmt.Printf("Dynamic Secret Targets (%d):\n\n", resp.Total)
	for _, t := range resp.Targets {
		fmt.Printf("  %s  %-20s  %-10s  %s:%d/%s\n", t.ID[:8], t.Name, t.DBType, t.Host, t.Port, t.DatabaseName)
	}
	return nil
}

func newDynTargetsCreateCmd() *cobra.Command {
	var name, description, dbType, host, database, username, password, sslMode string
	var port int
	var defaultTTL, maxTTL int
	var roles []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a dynamic secret database target",
		Example: `  ff vault dynamic targets create --name prod-db --db postgres --host db.example.com --port 5432 --database mydb --user admin --password secret`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynTargetsCreate(name, description, dbType, host, port, database, username, password, sslMode, roles, defaultTTL, maxTTL, asJSON)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Target name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().StringVar(&dbType, "db", "", "Database type: postgres, mysql (required)")
	cmd.Flags().StringVar(&host, "host", "", "Database host (required)")
	cmd.Flags().IntVar(&port, "port", 5432, "Database port")
	cmd.Flags().StringVar(&database, "database", "", "Database name (required)")
	cmd.Flags().StringVar(&username, "user", "", "Admin username (required)")
	cmd.Flags().StringVar(&password, "password", "", "Admin password (required)")
	cmd.Flags().StringVar(&sslMode, "ssl-mode", "require", "SSL mode")
	cmd.Flags().StringSliceVar(&roles, "roles", nil, "Allowed roles")
	cmd.Flags().IntVar(&defaultTTL, "default-ttl", 3600, "Default TTL in seconds")
	cmd.Flags().IntVar(&maxTTL, "max-ttl", 86400, "Max TTL in seconds")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("db")
	_ = cmd.MarkFlagRequired("host")
	_ = cmd.MarkFlagRequired("database")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func runDynTargetsCreate(name, description, dbType, host string, port int, database, username, password, sslMode string, roles []string, defaultTTL, maxTTL int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"name":               name,
		"description":        description,
		"db_type":            dbType,
		"host":               host,
		"port":               port,
		"database_name":      database,
		"admin_username":     username,
		"admin_password":     password,
		"ssl_mode":           sslMode,
		"default_ttl_seconds": defaultTTL,
		"max_ttl_seconds":    maxTTL,
	}
	if len(roles) > 0 {
		body["allowed_roles"] = roles
	}
	var target DynTarget
	if err := client.Post("/v1/vault/dynamic-secret-targets", body, &target); err != nil {
		return fmt.Errorf("could not create target: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(target)
		return nil
	}
	fmt.Printf("✅ Created dynamic secret target:\n")
	fmt.Printf("   ID:       %s\n", target.ID)
	fmt.Printf("   Name:     %s\n", target.Name)
	fmt.Printf("   Type:     %s\n", target.DBType)
	fmt.Printf("   Host:     %s:%d/%s\n", target.Host, target.Port, target.DatabaseName)
	return nil
}

func newDynTargetsTestCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "test <id>",
		Short: "Test connectivity to a dynamic secret target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynTargetsTest(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDynTargetsTest(id string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var result struct {
		OK        bool   `json:"ok"`
		Username  string `json:"username"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := client.Post("/v1/vault/dynamic-secret-targets/"+id+"/test", nil, &result); err != nil {
		return fmt.Errorf("could not test target: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	if result.OK {
		fmt.Printf("✅ Target %s connectivity OK\n", id)
	} else {
		fmt.Printf("❌ Target %s connectivity failed\n", id)
	}
	return nil
}

func newDynTargetsDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a dynamic secret target",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynTargetsDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runDynTargetsDelete(id string, force bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if !force && !YesMode {
		if !PromptConfirm(fmt.Sprintf("Delete dynamic secret target %s?", id), false) {
			fmt.Println("Aborted.")
			return nil
		}
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if err := client.Delete("/v1/vault/dynamic-secret-targets/"+id, nil); err != nil {
		return fmt.Errorf("could not delete target: %w", err)
	}
	fmt.Printf("✅ Deleted target %s\n", id)
	return nil
}

// ── Credentials ─────────────────────────────────────────────────────────────

func NewVaultDynamicCredentialsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credentials",
		Aliases: []string{"cred", "creds"},
		Short:   "Manage dynamic credential templates",
		Example: `  ff vault dynamic credentials list
  ff vault dynamic credentials create --target <target-id> --name readonly
  ff vault dynamic credentials generate <id>
  ff vault dynamic credentials revoke <id>`,
	}
	cmd.AddCommand(
		newDynCredentialsListCmd(),
		newDynCredentialsCreateCmd(),
		newDynCredentialsGenerateCmd(),
		newDynCredentialsRevokeCmd(),
	)
	return cmd
}

type DynCredential struct {
	ID           string `json:"id"`
	TargetID     string `json:"target_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	RoleTemplate string `json:"role_template"`
	TTLSeconds   int    `json:"ttl_seconds"`
	MaxTTLSeconds int   `json:"max_ttl_seconds"`
	CreatedAt    string `json:"created_at"`
}

func newDynCredentialsListCmd() *cobra.Command {
	var limit, offset int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List dynamic credential templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynCredentialsList(limit, offset, asJSON)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Max results")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDynCredentialsList(limit, offset int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var resp struct {
		Credentials []DynCredential `json:"credentials"`
		Total       int             `json:"total"`
	}
	path := fmt.Sprintf("/v1/vault/dynamic-credentials?limit=%d&offset=%d", limit, offset)
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not list credentials: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Credentials) == 0 {
		fmt.Println("No dynamic credential templates found.")
		return nil
	}
	fmt.Printf("Dynamic Credential Templates (%d):\n\n", resp.Total)
	for _, c := range resp.Credentials {
		fmt.Printf("  %s  %-20s  target=%s  ttl=%ds\n", c.ID[:8], c.Name, c.TargetID[:8], c.TTLSeconds)
	}
	return nil
}

func newDynCredentialsCreateCmd() *cobra.Command {
	var targetID, name, description, roleTemplate string
	var ttl, maxTTL int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a dynamic credential template",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynCredentialsCreate(targetID, name, description, roleTemplate, ttl, maxTTL, asJSON)
		},
	}
	cmd.Flags().StringVar(&targetID, "target", "", "Target ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Credential name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().StringVar(&roleTemplate, "role-template", "", "SQL role template")
	cmd.Flags().IntVar(&ttl, "ttl", 3600, "TTL in seconds")
	cmd.Flags().IntVar(&maxTTL, "max-ttl", 86400, "Max TTL in seconds")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("target")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runDynCredentialsCreate(targetID, name, description, roleTemplate string, ttl, maxTTL int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"target_id":       targetID,
		"name":            name,
		"description":     description,
		"ttl_seconds":     ttl,
		"max_ttl_seconds": maxTTL,
	}
	if roleTemplate != "" {
		body["role_template"] = roleTemplate
	}
	var cred DynCredential
	if err := client.Post("/v1/vault/dynamic-credentials", body, &cred); err != nil {
		return fmt.Errorf("could not create credential template: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(cred)
		return nil
	}
	fmt.Printf("✅ Created credential template:\n")
	fmt.Printf("   ID:     %s\n", cred.ID)
	fmt.Printf("   Name:   %s\n", cred.Name)
	fmt.Printf("   Target: %s\n", cred.TargetID)
	return nil
}

func newDynCredentialsGenerateCmd() *cobra.Command {
	var ttl int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "generate <id>",
		Short: "Generate fresh dynamic credentials",
		Long: `Generate a new set of short-lived database credentials from a credential template.
The password is shown only once — save it immediately.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynCredentialsGenerate(args[0], ttl, asJSON)
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 0, "Override TTL in seconds (0 = use template default)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDynCredentialsGenerate(id string, ttl int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if ttl > 0 {
		body["ttl_seconds"] = ttl
	}
	var result struct {
		LeaseID    string        `json:"lease_id"`
		Username   string        `json:"username"`
		Password   string        `json:"password"`
		Host       string        `json:"host"`
		Port       int           `json:"port"`
		Database   string        `json:"database"`
		ExpiresAt  string        `json:"expires_at"`
		Credential DynCredential `json:"credential"`
		Target     DynTarget     `json:"target"`
	}
	if err := client.Post("/v1/vault/dynamic-credentials/"+id+"/generate", body, &result); err != nil {
		return fmt.Errorf("could not generate credentials: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Generated dynamic credentials:\n\n")
	fmt.Printf("  Lease ID:  %s\n", result.LeaseID)
	fmt.Printf("  Host:      %s\n", result.Host)
	fmt.Printf("  Port:      %d\n", result.Port)
	fmt.Printf("  Database:  %s\n", result.Database)
	fmt.Printf("  Username:  %s\n", result.Username)
	fmt.Printf("  Expires:   %s\n", parseTime(result.ExpiresAt))
	if result.Password != "" {
		fmt.Printf("\n  ⚠️  Password (shown once): %s\n", result.Password)
		fmt.Printf("  Save this password now — it cannot be retrieved again.\n")
	}
	return nil
}

func newDynCredentialsRevokeCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke all active leases for a credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynCredentialsRevoke(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runDynCredentialsRevoke(id string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var result struct {
		CredentialID string `json:"credential_id"`
		Revoked      int    `json:"revoked"`
	}
	if err := client.Post("/v1/vault/dynamic-credentials/"+id+"/revoke", nil, &result); err != nil {
		return fmt.Errorf("could not revoke credentials: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Revoked %d lease(s) for credential %s\n", result.Revoked, id)
	return nil
}

// ── Leases ──────────────────────────────────────────────────────────────────

func NewVaultDynamicLeasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases",
		Aliases: []string{"lease"},
		Short:   "Manage dynamic credential leases",
		Example: `  ff vault dynamic leases renew --credential <cred-id> --lease <lease-id>
  ff vault dynamic leases revoke --credential <cred-id> --lease <lease-id>`,
	}
	cmd.AddCommand(
		newDynLeasesRenewCmd(),
		newDynLeasesRevokeCmd(),
	)
	return cmd
}

func newDynLeasesRenewCmd() *cobra.Command {
	var credentialID, leaseID string
	var ttl int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "renew",
		Short: "Renew a dynamic credential lease",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynLeasesRenew(credentialID, leaseID, ttl, asJSON)
		},
	}
	cmd.Flags().StringVar(&credentialID, "credential", "", "Credential ID (required)")
	cmd.Flags().StringVar(&leaseID, "lease", "", "Lease ID (required)")
	cmd.Flags().IntVar(&ttl, "ttl", 0, "New TTL in seconds (0 = keep current)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("credential")
	_ = cmd.MarkFlagRequired("lease")
	return cmd
}

func runDynLeasesRenew(credentialID, leaseID string, ttl int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if ttl > 0 {
		body["ttl_seconds"] = ttl
	}
	path := fmt.Sprintf("/v1/vault/dynamic-credentials/%s/leases/%s/renew", credentialID, leaseID)
	var result struct {
		LeaseID   string `json:"lease_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := client.Post(path, body, &result); err != nil {
		return fmt.Errorf("could not renew lease: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Renewed lease %s (expires: %s)\n", result.LeaseID, parseTime(result.ExpiresAt))
	return nil
}

func newDynLeasesRevokeCmd() *cobra.Command {
	var credentialID, leaseID string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a specific dynamic credential lease",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDynLeasesRevoke(credentialID, leaseID, asJSON)
		},
	}
	cmd.Flags().StringVar(&credentialID, "credential", "", "Credential ID (required)")
	cmd.Flags().StringVar(&leaseID, "lease", "", "Lease ID (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("credential")
	_ = cmd.MarkFlagRequired("lease")
	return cmd
}

func runDynLeasesRevoke(credentialID, leaseID string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/vault/dynamic-credentials/%s/leases/%s/revoke", credentialID, leaseID)
	var result struct {
		LeaseID string `json:"lease_id"`
		Revoked bool   `json:"revoked"`
	}
	if err := client.Post(path, nil, &result); err != nil {
		return fmt.Errorf("could not revoke lease: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Revoked lease %s\n", leaseID)
	return nil
}
