package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewVaultVersionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "versions",
		Aliases: []string{"version", "ver"},
		Short:   "Manage vault secret versions",
		Example: `  ff vault versions list --secret <id>
  ff vault versions get --secret <id> --version 3
  ff vault versions diff --secret <id> --from 2 --to 4
  ff vault versions rollback --secret <id> --to 3`,
	}
	cmd.AddCommand(
		newVaultVersionsListCmd(),
		newVaultVersionsGetCmd(),
		newVaultVersionsDiffCmd(),
		newVaultVersionsRollbackCmd(),
	)
	return cmd
}

type VaultVersion struct {
	Version        int    `json:"version"`
	SecretID       string `json:"secret_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	SecretType     string `json:"secret_type"`
	Namespace      string `json:"namespace"`
	EncryptedValue bool   `json:"encrypted_value_changed"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	Reason         string `json:"reason"`
}

type ListVersionsResponse struct {
	Versions []VaultVersion `json:"versions"`
	Total    int64          `json:"total"`
	Limit    int            `json:"limit"`
	Offset   int            `json:"offset"`
}

type VersionDiff struct {
	FromVersion          int  `json:"from_version"`
	ToVersion            int  `json:"to_version"`
	HasChanges           bool `json:"has_changes"`
	NameChanged          bool `json:"name_changed"`
	DescriptionChanged   bool `json:"description_changed"`
	ScopesChanged        bool `json:"scopes_changed"`
	EncryptedValueChanged bool `json:"encrypted_value_changed"`
}

func newVaultVersionsListCmd() *cobra.Command {
	var secretID string
	var limit, offset int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List versions of a vault secret",
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultVersionsList(secretID, limit, offset, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max results (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	return cmd
}

func runVaultVersionsList(secretID string, limit, offset int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/vault/secrets/%s/versions?limit=%d&offset=%d", secretID, limit, offset)
	var resp ListVersionsResponse
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not list versions: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Versions) == 0 {
		fmt.Println("No versions found.")
		return nil
	}
	fmt.Printf("Versions for secret %s (%d total):\n\n", secretID, resp.Total)
	for _, v := range resp.Versions {
		reason := v.Reason
		if reason == "" {
			reason = "—"
		}
		fmt.Printf("  v%d  by=%-12s  %s  reason=%s\n", v.Version, v.CreatedBy, parseTime(v.CreatedAt), reason)
	}
	return nil
}

func newVaultVersionsGetCmd() *cobra.Command {
	var secretID string
	var version int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a specific version of a vault secret",
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultVersionsGet(secretID, version, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().IntVar(&version, "version", 0, "Version number (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func runVaultVersionsGet(secretID string, version int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/vault/secrets/%s/versions/%d", secretID, version)
	var ver VaultVersion
	if err := client.Get(path, &ver); err != nil {
		return fmt.Errorf("could not get version: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(ver)
		return nil
	}
	fmt.Printf("Version %d of secret %s:\n\n", ver.Version, secretID)
	fmt.Printf("  Name:        %s\n", ver.Name)
	fmt.Printf("  Description: %s\n", ver.Description)
	fmt.Printf("  Type:        %s\n", ver.SecretType)
	fmt.Printf("  Namespace:   %s\n", ver.Namespace)
	fmt.Printf("  Created by:  %s\n", ver.CreatedBy)
	fmt.Printf("  Created at:  %s\n", parseTime(ver.CreatedAt))
	if ver.Reason != "" {
		fmt.Printf("  Reason:      %s\n", ver.Reason)
	}
	return nil
}

func newVaultVersionsDiffCmd() *cobra.Command {
	var secretID string
	var fromVersion, toVersion int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff two versions of a vault secret",
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultVersionsDiff(secretID, fromVersion, toVersion, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().IntVar(&fromVersion, "from", 0, "From version (required)")
	cmd.Flags().IntVar(&toVersion, "to", 0, "To version (defaults to current)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func runVaultVersionsDiff(secretID string, fromVersion, toVersion int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/vault/secrets/%s/versions/diff?from_version=%d", secretID, fromVersion)
	if toVersion > 0 {
		path += fmt.Sprintf("&to_version=%d", toVersion)
	}
	var diff VersionDiff
	if err := client.Get(path, &diff); err != nil {
		return fmt.Errorf("could not diff versions: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(diff)
		return nil
	}
	toLabel := "current"
	if toVersion > 0 {
		toLabel = fmt.Sprintf("v%d", toVersion)
	}
	fmt.Printf("Diff: v%d → %s for secret %s\n\n", fromVersion, toLabel, secretID)
	if !diff.HasChanges {
		fmt.Println("  No changes detected.")
		return nil
	}
	if diff.NameChanged {
		fmt.Println("  ~ Name changed")
	}
	if diff.DescriptionChanged {
		fmt.Println("  ~ Description changed")
	}
	if diff.ScopesChanged {
		fmt.Println("  ~ Scopes changed")
	}
	if diff.EncryptedValueChanged {
		fmt.Println("  ~ Encrypted value changed")
	}
	return nil
}

func newVaultVersionsRollbackCmd() *cobra.Command {
	var secretID string
	var targetVersion int
	var reason string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Rollback a vault secret to a previous version",
		Example: `  ff vault versions rollback --secret <id> --to 3
  ff vault versions rollback --secret <id> --to 2 --reason "revert broken rotation"`,
		RunE: func(_ *cobra.Command, args []string) error {
			return runVaultVersionsRollback(secretID, targetVersion, reason, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Secret ID (required)")
	cmd.Flags().IntVar(&targetVersion, "to", 0, "Target version to rollback to (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for rollback")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("secret")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func runVaultVersionsRollback(secretID string, targetVersion int, reason string, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"target_version": targetVersion,
	}
	if reason != "" {
		body["reason"] = reason
	}
	var result struct {
		Secret        VaultSecret `json:"secret"`
		NewVersion    VaultVersion `json:"new_version"`
		RolledBackTo  int          `json:"rolled_back_to"`
		Message       string       `json:"message"`
	}
	if err := client.Post("/v1/vault/secrets/"+secretID+"/rollback", body, &result); err != nil {
		return fmt.Errorf("could not rollback: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}
	fmt.Printf("✅ Rolled back secret %s to version %d\n", secretID, result.RolledBackTo)
	if result.Message != "" {
		fmt.Printf("   %s\n", result.Message)
	}
	return nil
}
