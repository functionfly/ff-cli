package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var billingCmd = &cobra.Command{
	Use:     "billing",
	Aliases: []string{"plan"},
	Short:   "Manage billing and plan",
	Long: `View your current plan, usage, and manage subscription upgrades.

Shows the current plan tier, included limits, usage this period, and
provides upgrade/downgrade options.`,
	Example: `  ff billing
  ff billing show
  ff billing upgrade --plan pro
  ff billing downgrade --plan free`,
	SilenceUsage: true,
}

func init() {
	billingCmd.AddCommand(newBillingShowCmd())
	billingCmd.AddCommand(newBillingUpgradeCmd())
	billingCmd.AddCommand(newBillingDowngradeCmd())
	billingCmd.AddCommand(newBillingUsageCmd())
}

func BillingCmd() *cobra.Command {
	return billingCmd
}

type BillingInfo struct {
	Plan        string         `json:"plan"`
	Status      string         `json:"status,omitempty"`
	PeriodStart string         `json:"period_start,omitempty"`
	PeriodEnd   string         `json:"period_end,omitempty"`
	Limits      PlanLimits     `json:"limits"`
	Usage       PlanUsage      `json:"usage"`
	Available   []PlanOption   `json:"available_plans,omitempty"`
}

type PlanLimits struct {
	Functions     int `json:"functions"`
	Invocations   int `json:"invocations_per_month"`
	ExecutionTime int `json:"execution_time_ms"`
	MemoryMB      int `json:"memory_mb"`
	TeamMembers   int `json:"team_members"`
	CustomDomains int `json:"custom_domains"`
}

type PlanUsage struct {
	Functions     int     `json:"functions"`
	Invocations   int64   `json:"invocations"`
	InvocationsPct float64 `json:"invocations_pct,omitempty"`
	Bandwidth     int64   `json:"bandwidth_bytes,omitempty"`
	Storage       int64   `json:"storage_bytes,omitempty"`
}

type PlanOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Price    string `json:"price"`
	Interval string `json:"interval,omitempty"`
}

func newBillingShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "show",
		Aliases: []string{"status", "info"},
		Short:   "Show current plan and usage",
		Long:    `Display the current subscription plan, usage this billing period, and limits.`,
		Example: `  ff billing show
  ff billing
  ff billing --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBillingShow(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runBillingShow(asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var info BillingInfo
	if err := client.Get("/v1/billing", &info); err != nil {
		return fmt.Errorf("could not fetch billing info: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(info)
		return nil
	}

	plan := info.Plan
	if plan == "" {
		plan = "free"
	}
	fmt.Printf("\n💳 Plan: %s\n", planDisplayName(plan))
	fmt.Println(strings.Repeat("─", 50))

	if info.Status != "" {
		fmt.Printf("  Status:   %s\n", info.Status)
	}
	if info.PeriodStart != "" {
		fmt.Printf("  Period:   %s → %s\n", info.PeriodStart, info.PeriodEnd)
	}

	fmt.Printf("\n  Usage this period:\n")
	fmt.Printf("    Functions:     %d", info.Usage.Functions)
	if info.Limits.Functions > 0 {
		fmt.Printf(" / %d", info.Limits.Functions)
	}
	fmt.Println()

	fmt.Printf("    Invocations:   %s", formatNumber(info.Usage.Invocations))
	if info.Limits.Invocations > 0 {
		fmt.Printf(" / %s", formatNumber(int64(info.Limits.Invocations)))
		if info.Usage.InvocationsPct > 0 {
			fmt.Printf("  (%.1f%%)", info.Usage.InvocationsPct)
		}
	}
	fmt.Println()

	if info.Limits.ExecutionTime > 0 {
		fmt.Printf("    Exec time:     %d ms max\n", info.Limits.ExecutionTime)
	}
	if info.Limits.MemoryMB > 0 {
		fmt.Printf("    Memory:        %d MB max\n", info.Limits.MemoryMB)
	}
	if info.Limits.TeamMembers > 0 {
		fmt.Printf("    Team members:  %d\n", info.Limits.TeamMembers)
	}

	if len(info.Available) > 0 {
		fmt.Printf("\n  Available plans:\n")
		for _, p := range info.Available {
			price := p.Price
			if price == "" {
				price = "free"
			}
			if p.Interval != "" {
				price += "/" + p.Interval
			}
			fmt.Printf("    %-16s %s\n", p.Name, price)
		}
		fmt.Printf("\n  Upgrade: ff billing upgrade --plan <id>\n")
	}

	fmt.Println()
	return nil
}

func newBillingUpgradeCmd() *cobra.Command {
	var plan string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade to a higher plan",
		Long: `Upgrade your subscription to a higher tier plan. The change takes
effect immediately and is prorated for the current billing period.`,
		Example: `  ff billing upgrade --plan starter
  ff billing upgrade --plan professional
  ff billing upgrade --plan enterprise --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBillingChange(plan, "upgrade", asJSON)
		},
	}
	cmd.Flags().StringVar(&plan, "plan", "", "Target plan: starter, professional, enterprise (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("plan")
	return cmd
}

func newBillingDowngradeCmd() *cobra.Command {
	var plan string
	var asJSON bool
	var force bool
	cmd := &cobra.Command{
		Use:   "downgrade",
		Short: "Downgrade to a lower plan",
		Long: `Downgrade your subscription to a lower tier. The change takes effect
at the end of the current billing period. Features above the new plan
limit will become read-only.`,
		Example: `  ff billing downgrade --plan free
  ff billing downgrade --plan starter --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBillingChange(plan, "downgrade", asJSON)
		},
	}
	cmd.Flags().StringVar(&plan, "plan", "", "Target plan: free, starter (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("plan")
	return cmd
}

func runBillingChange(plan, action string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}

	if action == "downgrade" && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Downgrade to %s? This takes effect at period end.", planDisplayName(plan)),
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

	body := map[string]interface{}{
		"plan":   plan,
		"action": action,
	}
	var result BillingInfo
	if err := client.Post("/v1/billing/subscription", body, &result); err != nil {
		return fmt.Errorf("could not %s plan: %w", action, err)
	}

	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}

	if action == "upgrade" {
		fmt.Printf("✅ Upgraded to %s\n", planDisplayName(plan))
		fmt.Printf("   Changes are effective immediately.\n")
	} else {
		fmt.Printf("✅ Downgrade scheduled to %s\n", planDisplayName(plan))
		fmt.Printf("   Changes take effect at the end of the billing period.\n")
	}
	return nil
}

func newBillingUsageCmd() *cobra.Command {
	var asJSON bool
	var period string
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show detailed usage breakdown",
		Long:  `Show a detailed breakdown of usage for the current billing period.`,
		Example: `  ff billing usage
  ff billing usage --period 30d
  ff billing usage --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBillingUsage(period, asJSON)
		},
	}
	cmd.Flags().StringVar(&period, "period", "current", "Period: current, 30d, 90d")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

type UsageDetail struct {
	Period      string           `json:"period"`
	Invocations int64            `json:"invocations"`
	Errors      int64            `json:"errors"`
	AvgLatency  float64          `json:"avg_latency_ms"`
	Bandwidth   int64            `json:"bandwidth_bytes"`
	Storage     int64            `json:"storage_bytes"`
	TopFunctions []FunctionUsage `json:"top_functions,omitempty"`
}

type FunctionUsage struct {
	Name        string `json:"name"`
	Author      string `json:"author"`
	Invocations int64  `json:"invocations"`
	Errors      int64  `json:"errors"`
	AvgLatency  float64 `json:"avg_latency_ms"`
}

func runBillingUsage(period string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var detail UsageDetail
	path := "/v1/billing/usage"
	if period != "current" {
		path += "?period=" + period
	}
	if err := client.Get(path, &detail); err != nil {
		return fmt.Errorf("could not fetch usage: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(detail)
		return nil
	}

	fmt.Printf("\n📊 Usage (%s)\n", detail.Period)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  Invocations:  %s\n", formatNumber(detail.Invocations))
	fmt.Printf("  Errors:       %s\n", formatNumber(detail.Errors))
	fmt.Printf("  Avg latency:  %.0f ms\n", detail.AvgLatency)
	if detail.Bandwidth > 0 {
		fmt.Printf("  Bandwidth:    %s\n", formatBytesBilling(detail.Bandwidth))
	}
	if detail.Storage > 0 {
		fmt.Printf("  Storage:      %s\n", formatBytesBilling(detail.Storage))
	}

	if len(detail.TopFunctions) > 0 {
		fmt.Printf("\n  Top functions:\n")
		for i, fn := range detail.TopFunctions {
			if i >= 10 {
				break
			}
			fnName := fn.Author + "/" + fn.Name
			fmt.Printf("    %-30s  %s calls", fnName, formatNumber(fn.Invocations))
			if fn.Errors > 0 {
				fmt.Printf("  (%s errors)", formatNumber(fn.Errors))
			}
			fmt.Println()
		}
	}

	fmt.Println()
	return nil
}

func formatBytesBilling(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
