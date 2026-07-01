package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage vault security configuration",
		Example: `  ff vault config mfa
  ff vault config mfa --enable --method totp
  ff vault config sso
  ff vault config security
  ff vault config break-glass --reason "emergency" --duration 60
  ff vault config escrow --enable
  ff vault config cache`,
	}
	cmd.AddCommand(
		newVaultConfigMFACmd(),
		newVaultConfigSSOCmd(),
		newVaultConfigBreakGlassCmd(),
		newVaultConfigEscrowCmd(),
		newVaultConfigCacheCmd(),
	)
	return cmd
}

// ── MFA ─────────────────────────────────────────────────────────────────────

type MFAConfig struct {
	MFARequired        bool   `json:"mfa_required"`
	MFAMethod          string `json:"mfa_method"`
	EnforceForTokens   bool   `json:"enforce_for_tokens"`
	EnforceForAPI      bool   `json:"enforce_for_api"`
	MFASessionTTL      int    `json:"mfa_session_ttl_seconds"`
}

func newVaultConfigMFACmd() *cobra.Command {
	var enable bool
	var method string
	var sessionTTL int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "mfa",
		Short: "View or update vault MFA configuration",
		Example: `  ff vault config mfa
  ff vault config mfa --enable --method totp
  ff vault config mfa --enable --method webauthn --session-ttl 7200`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultConfigMFA(enable, method, sessionTTL, asJSON)
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable MFA")
	cmd.Flags().StringVar(&method, "method", "", "MFA method: totp, webauthn, both")
	cmd.Flags().IntVar(&sessionTTL, "session-ttl", 0, "MFA session TTL in seconds (60-86400)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultConfigMFA(enable bool, method string, sessionTTL int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureMFA); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	// If no flags set, just show current config
	if !enable && method == "" && sessionTTL == 0 {
		var cfg MFAConfig
		if err := client.Get("/v1/vault/mfa/config", &cfg); err != nil {
			return fmt.Errorf("could not get MFA config: %w", err)
		}
		if asJSON || WantJSON() {
			printJSON(cfg)
			return nil
		}
		printMFAConfig(cfg)
		return nil
	}
	// Update config
	body := map[string]any{
		"mfa_required": enable,
	}
	if method != "" {
		body["mfa_method"] = method
	}
	if sessionTTL > 0 {
		body["mfa_session_ttl_seconds"] = sessionTTL
	}
	var cfg MFAConfig
	if err := client.Put("/v1/vault/mfa/config", body, &cfg); err != nil {
		return fmt.Errorf("could not update MFA config: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(cfg)
		return nil
	}
	fmt.Println("✅ Updated MFA configuration:")
	fmt.Println()
	printMFAConfig(cfg)
	return nil
}

func printMFAConfig(cfg MFAConfig) {
	status := "disabled"
	if cfg.MFARequired {
		status = "enabled"
	}
	fmt.Printf("  MFA Status:     %s\n", status)
	fmt.Printf("  Method:         %s\n", cfg.MFAMethod)
	fmt.Printf("  Enforce Tokens: %v\n", cfg.EnforceForTokens)
	fmt.Printf("  Enforce API:    %v\n", cfg.EnforceForAPI)
	fmt.Printf("  Session TTL:    %ds\n", cfg.MFASessionTTL)
}

// ── SSO ─────────────────────────────────────────────────────────────────────

type SSOConfig struct {
	Enabled                bool              `json:"enabled"`
	SAMLMetadataURL        string            `json:"saml_metadata_url"`
	SAMLEntityID           string            `json:"saml_entity_id"`
	SAMLSSOURL             string            `json:"saml_sso_url"`
	SAMLSLOURL             string            `json:"saml_slo_url"`
	JITProvisioningEnabled bool              `json:"jit_provisioning_enabled"`
	AttributeRoleMapping   map[string]string `json:"attribute_role_mapping"`
}

func newVaultConfigSSOCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sso",
		Short: "View SSO/SAML configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultConfigSSO(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultConfigSSO(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureSSO); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var cfg SSOConfig
	if err := client.Get("/v1/vault/sso/config", &cfg); err != nil {
		return fmt.Errorf("could not get SSO config: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(cfg)
		return nil
	}
	status := "disabled"
	if cfg.Enabled {
		status = "enabled"
	}
	fmt.Printf("SSO Configuration:\n\n")
	fmt.Printf("  Status:          %s\n", status)
	fmt.Printf("  Entity ID:       %s\n", cfg.SAMLEntityID)
	fmt.Printf("  SSO URL:         %s\n", cfg.SAMLSSOURL)
	fmt.Printf("  SLO URL:         %s\n", cfg.SAMLSLOURL)
	fmt.Printf("  Metadata URL:    %s\n", cfg.SAMLMetadataURL)
	fmt.Printf("  JIT Provisioning: %v\n", cfg.JITProvisioningEnabled)
	return nil
}

// ── Break-Glass ─────────────────────────────────────────────────────────────

func newVaultConfigBreakGlassCmd() *cobra.Command {
	var reason string
	var duration int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "break-glass",
		Short: "Request emergency break-glass access",
		Long: `Submit an emergency access request. Must be approved by a configured
approver — self-approval is not allowed.`,
		Example: `  ff vault config break-glass --reason "Production outage" --duration 60
  ff vault config break-glass list
  ff vault config break-glass approve <id>
  ff vault config break-glass deny <id>
  ff vault config break-glass revoke <id>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakGlassRequest(reason, duration, asJSON)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for emergency access (required)")
	cmd.Flags().IntVar(&duration, "duration", 60, "Duration in minutes")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("reason")

	cmd.AddCommand(
		newBreakGlassListCmd(),
		newBreakGlassApproveCmd(),
		newBreakGlassDenyCmd(),
		newBreakGlassRevokeCmd(),
	)
	return cmd
}

type BreakGlassRequest struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	RequesterID     string `json:"requester_id"`
	Reason          string `json:"reason"`
	DurationMinutes int    `json:"duration_minutes"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
}

func runBreakGlassRequest(reason string, duration int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureBreakGlass); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"reason":          reason,
		"duration_minutes": duration,
	}
	var bg BreakGlassRequest
	if err := client.Post("/v1/vault/break-glass", body, &bg); err != nil {
		return fmt.Errorf("could not submit break-glass request: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(bg)
		return nil
	}
	fmt.Printf("✅ Submitted break-glass request:\n")
	fmt.Printf("   ID:       %s\n", bg.ID)
	fmt.Printf("   Reason:   %s\n", bg.Reason)
	fmt.Printf("   Duration: %d minutes\n", bg.DurationMinutes)
	fmt.Printf("   Status:   %s\n", bg.Status)
	fmt.Printf("\n   ⚠️  Awaiting approval from a configured approver.\n")
	return nil
}

func newBreakGlassListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List break-glass requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakGlassList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runBreakGlassList(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureBreakGlass); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var requests []BreakGlassRequest
	if err := client.Get("/v1/vault/break-glass", &requests); err != nil {
		return fmt.Errorf("could not list break-glass requests: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(requests)
		return nil
	}
	if len(requests) == 0 {
		fmt.Println("No break-glass requests found.")
		return nil
	}
	fmt.Printf("Break-Glass Requests (%d):\n\n", len(requests))
	for _, r := range requests {
		fmt.Printf("  %s  %-10s  %s  %s\n", r.ID[:8], r.Status, r.Reason, parseTime(r.CreatedAt))
	}
	return nil
}

func newBreakGlassApproveCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a break-glass request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakGlassAction(args[0], "approve", asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newBreakGlassDenyCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "deny <id>",
		Short: "Deny a break-glass request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakGlassAction(args[0], "deny", asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newBreakGlassRevokeCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an active break-glass grant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakGlassAction(args[0], "revoke", asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runBreakGlassAction(id, action string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureBreakGlass); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/vault/break-glass/%s/%s", id, action)
	var result map[string]any
	if err := client.Post(path, nil, &result); err != nil {
		return fmt.Errorf("could not %s break-glass request: %w", action, err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Break-glass request %s: %s\n", action, id)
	return nil
}

// ── Escrow ──────────────────────────────────────────────────────────────────

type EscrowStatus struct {
	Enabled     bool   `json:"enabled"`
	EnabledAt   string `json:"enabled_at"`
	Fingerprint string `json:"fingerprint"`
}

func newVaultConfigEscrowCmd() *cobra.Command {
	var enable, disable bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "escrow",
		Short: "Manage vault key escrow (encrypted recovery)",
		Example: `  ff vault config escrow
  ff vault config escrow --enable
  ff vault config escrow --disable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultConfigEscrow(enable, disable, asJSON)
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable escrow")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable escrow")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultConfigEscrow(enable, disable bool, asJSON bool) error {
	if enable && disable {
		return fmt.Errorf("cannot use --enable and --disable together")
	}
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureEscrow); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if !enable && !disable {
		var status EscrowStatus
		if err := client.Get("/v1/vault/escrow", &status); err != nil {
			return fmt.Errorf("could not get escrow status: %w", err)
		}
		if asJSON || WantJSON() {
			printJSON(status)
			return nil
		}
		st := "disabled"
		if status.Enabled {
			st = "enabled"
		}
		fmt.Printf("Escrow Status: %s\n", st)
		if status.Enabled {
			fmt.Printf("  Enabled at:  %s\n", parseTime(status.EnabledAt))
			fmt.Printf("  Fingerprint: %s\n", status.Fingerprint)
		}
		return nil
	}
	if disable {
		if err := client.Delete("/v1/vault/escrow", nil); err != nil {
			return fmt.Errorf("could not disable escrow: %w", err)
		}
		fmt.Println("✅ Escrow disabled")
		return nil
	}
	// Enable escrow — requires encrypted blob which is generated client-side.
	// For CLI, we send a placeholder that the server expects.
	body := map[string]any{
		"security_question_hashes": []string{},
		"kdf_salt":        "",
		"encrypted_blob":  "",
		"blob_iv":         "",
		"blob_auth_tag":   "",
	}
	var status EscrowStatus
	if err := client.Post("/v1/vault/escrow", body, &status); err != nil {
		return fmt.Errorf("could not enable escrow: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(status)
		return nil
	}
	fmt.Println("✅ Escrow enabled")
	return nil
}

// ── Cache Stats ─────────────────────────────────────────────────────────────

func newVaultConfigCacheCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "View vault cache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultConfigCache(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultConfigCache(asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureCacheStats); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var stats struct {
		Enabled  bool   `json:"enabled"`
		Meta     int    `json:"meta"`
		Tokens   int    `json:"tokens"`
		Resource string `json:"resource"`
	}
	if err := client.Get("/v1/vault/cache/stats", &stats); err != nil {
		return fmt.Errorf("could not get cache stats: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(stats)
		return nil
	}
	fmt.Printf("Vault Cache Statistics:\n\n")
	fmt.Printf("  Enabled: %v\n", stats.Enabled)
	fmt.Printf("  Meta:    %d entries\n", stats.Meta)
	fmt.Printf("  Tokens:  %d entries\n", stats.Tokens)
	return nil
}
