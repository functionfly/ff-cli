package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func NewVaultAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "audit",
		Aliases: []string{"log", "logs"},
		Short:   "Query vault audit logs",
		Example: `  ff vault audit list
  ff vault audit list --limit 50
  ff vault audit list --secret <id>
  ff vault audit list --action create
  ff vault audit list --actor user --actor-id <uid>
  ff vault audit export --format json
  ff vault audit export --format csv --from 2025-01-01T00:00:00Z --to 2025-06-30T23:59:59Z`,
	}
	cmd.AddCommand(
		newVaultAuditListCmd(),
		newVaultAuditExportCmd(),
	)
	return cmd
}

type AuditLogEntry struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	SecretID   string `json:"secret_id"`
	SecretName string `json:"secret_name"`
	Action     string `json:"action"`
	ActorType  string `json:"actor_type"`
	ActorID    string `json:"actor_id"`
	IPAddress  string `json:"ip_address"`
	Success    bool   `json:"success"`
	Details    string `json:"details"`
	CreatedAt  string `json:"created_at"`
}

type AuditLogResponse struct {
	Entries []AuditLogEntry `json:"entries"`
	Total   int64           `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
}

func newVaultAuditListCmd() *cobra.Command {
	var secretID, action, actorType, actorID string
	var limit, offset int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List vault audit log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultAuditList(secretID, action, actorType, actorID, limit, offset, asJSON)
		},
	}
	cmd.Flags().StringVar(&secretID, "secret", "", "Filter by secret ID")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action (create, read, update, delete, revoke, rollback, ...)")
	cmd.Flags().StringVar(&actorType, "actor", "", "Filter by actor type (user, token)")
	cmd.Flags().StringVar(&actorID, "actor-id", "", "Filter by actor ID (use with --actor)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max results (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runVaultAuditList(secretID, action, actorType, actorID string, limit, offset int, asJSON bool) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var path string
	switch {
	case action != "":
		path = fmt.Sprintf("/v1/vault/audit/action/%s?limit=%d&offset=%d", action, limit, offset)
	case actorType != "" && actorID != "":
		path = fmt.Sprintf("/v1/vault/audit/actor/%s/%s?limit=%d&offset=%d", actorType, actorID, limit, offset)
	case secretID != "":
		path = fmt.Sprintf("/v1/vault/secrets/%s/audit?limit=%d&offset=%d", secretID, limit, offset)
	default:
		path = fmt.Sprintf("/v1/vault/audit?limit=%d&offset=%d", limit, offset)
	}

	var resp AuditLogResponse
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not fetch audit logs: %w", err)
	}
	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}
	if len(resp.Entries) == 0 {
		fmt.Println("No audit log entries found.")
		return nil
	}
	fmt.Printf("Vault Audit Log (%d total):\n\n", resp.Total)
	for _, e := range resp.Entries {
		status := "✅"
		if !e.Success {
			status = "❌"
		}
		actor := e.ActorType + ":" + e.ActorID
		if len(actor) > 20 {
			actor = actor[:17] + "..."
		}
		fmt.Printf("  %s %-8s  %-10s  %-20s  %s\n", status, e.Action, e.SecretName, actor, parseTime(e.CreatedAt))
		if e.Details != "" {
			fmt.Printf("             %s\n", e.Details)
		}
	}
	fmt.Printf("\nShowing %d of %d entries\n", len(resp.Entries), resp.Total)
	return nil
}

func newVaultAuditExportCmd() *cobra.Command {
	var format, from, to, secretID, action, outputPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export vault audit logs",
		Long: `Export audit logs in JSON, CSV, or CEF format.
By default exports the last 24 hours. Use --from and --to with RFC3339 timestamps.`,
		Example: `  ff vault audit export --format json
  ff vault audit export --format csv --output audit.csv
  ff vault audit export --format cef --from 2025-01-01T00:00:00Z`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultAuditExport(format, from, to, secretID, action, outputPath)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "Export format: json, csv, cef")
	cmd.Flags().StringVar(&from, "from", "", "Start time (RFC3339, default: 24h ago)")
	cmd.Flags().StringVar(&to, "to", "", "End time (RFC3339, default: now)")
	cmd.Flags().StringVar(&secretID, "secret", "", "Filter by secret ID")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: stdout)")
	return cmd
}

func runVaultAuditExport(format, from, to, secretID, action, outputPath string) error {
	if err := requireAuthN(); err != nil {
		return err
	}
	if err := requireVaultPlan(VaultFeatureAuditExport); err != nil {
		return err
	}
	format = strings.ToLower(format)
	if format != "json" && format != "csv" && format != "cef" {
		return fmt.Errorf("unsupported format %q — use json, csv, or cef", format)
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	params := fmt.Sprintf("?format=%s", format)
	if from != "" {
		params += "&from=" + from
	}
	if to != "" {
		params += "&to=" + to
	}
	if secretID != "" {
		params += "&secret_id=" + secretID
	}
	if action != "" {
		params += "&action=" + action
	}

	path := "/v1/vault/audit/export" + params
	resp, err := client.GetRaw(path)
	if err != nil {
		return fmt.Errorf("could not export audit logs: %w", err)
	}
	defer resp.Body.Close()

	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("could not create output file: %w", err)
		}
		defer f.Close()
		if _, err := copyWithLimit(f, resp.Body, maxResponseSize); err != nil {
			return fmt.Errorf("could not write output: %w", err)
		}
		rowCount := resp.Header.Get("X-Audit-Row-Count")
		fmt.Printf("✅ Exported %s audit log entries to %s\n", rowCount, outputPath)
		return nil
	}
	data, err := readWithLimit(resp.Body, maxResponseSize)
	if err != nil {
		return fmt.Errorf("could not read response: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
