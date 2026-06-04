package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// ── API types ────────────────────────────────────────────────────────────────

type CanaryConfig struct {
	ID               string    `json:"id"`
	FunctionID       string    `json:"function_id"`
	CanaryVersion    string    `json:"canary_version"`
	StableVersion    string    `json:"stable_version"`
	TrafficPercent   int       `json:"traffic_percent"`
	Status           string    `json:"status"`
	AutoPromote      bool      `json:"auto_promote"`
	PromoteThreshold float64   `json:"promote_threshold"`
	PromoteWindow    int       `json:"promote_window"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CanaryHistory struct {
	Configs []CanaryConfig `json:"configs"`
}

// ── Command ──────────────────────────────────────────────────────────────────

func NewCanaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "canary",
		Short: "Manage canary deployments",
		Long: `Gradually roll out a new version to a percentage of traffic.

  ff canary start --version 1.2.0 --percent 10   Start canary at 10%
  ff canary status                                Check current canary
  ff canary promote --percent 50                  Increase to 50%
  ff canary promote --full                        Promote to 100% (full rollout)
  ff canary rollback                              Roll back canary
  ff canary cancel                                Cancel and remove canary
  ff canary history                               Show past canary deployments`,
	}
	cmd.AddCommand(
		newCanaryStartCmd(),
		newCanaryStatusCmd(),
		newCanaryPromoteCmd(),
		newCanaryRollbackCmd(),
		newCanaryCancelCmd(),
		newCanaryHistoryCmd(),
	)
	return cmd
}

// ── start ────────────────────────────────────────────────────────────────────

func newCanaryStartCmd() *cobra.Command {
	var version string
	var percent int
	var autoPromote bool
	var promoteThreshold float64
	var promoteWindow int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "start [author/name]",
		Short: "Start a canary deployment",
		Example: "  ff canary start --version 1.2.0 --percent 10\n" +
			"  ff canary start --version 1.2.0 --percent 5 --auto-promote --promote-threshold 99.5",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if version == "" {
				return fmt.Errorf("--version is required")
			}
			if percent <= 0 || percent >= 100 {
				return fmt.Errorf("--percent must be between 1 and 99")
			}
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			req := map[string]interface{}{
				"version":           version,
				"traffic_percent":   percent,
				"auto_promote":      autoPromote,
				"promote_threshold": promoteThreshold,
				"promote_window":    promoteWindow,
			}
			var canary CanaryConfig
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary", author, name)
			if err := client.Post(path, req, &canary); err != nil {
				return fmt.Errorf("could not start canary: %w", err)
			}
			if asJSON || WantJSON() {
				printJSON(canary)
				return nil
			}
			fmt.Printf("🐤 Canary started for %s/%s\n\n", author, name)
			printCanaryStatus(&canary)
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "New version to deploy as canary (required)")
	cmd.Flags().IntVar(&percent, "percent", 10, "Initial traffic percentage (1–99)")
	cmd.Flags().BoolVar(&autoPromote, "auto-promote", false, "Automatically promote when success threshold is met")
	cmd.Flags().Float64Var(&promoteThreshold, "promote-threshold", 99.0, "Success rate threshold for auto-promotion (0–100)")
	cmd.Flags().IntVar(&promoteWindow, "promote-window", 30, "Observation window in minutes before auto-promote")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// ── status ───────────────────────────────────────────────────────────────────

func newCanaryStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "status [author/name]",
		Short:   "Show the current canary deployment",
		Example: "  ff canary status\n  ff canary status alice/my-fn",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			var canary CanaryConfig
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary", author, name)
			if err := client.Get(path, &canary); err != nil {
				return fmt.Errorf("could not get canary status: %w", err)
			}
			if asJSON || WantJSON() {
				printJSON(canary)
				return nil
			}
			fmt.Printf("Canary for %s/%s\n\n", author, name)
			printCanaryStatus(&canary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// ── promote ──────────────────────────────────────────────────────────────────

func newCanaryPromoteCmd() *cobra.Command {
	var percent int
	var full bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "promote [author/name]",
		Short: "Increase canary traffic or fully promote",
		Example: "  ff canary promote --percent 50\n" +
			"  ff canary promote --full",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			if full {
				var canary CanaryConfig
				path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary/promote", author, name)
				if err := client.Post(path, map[string]interface{}{}, &canary); err != nil {
					return fmt.Errorf("could not promote canary: %w", err)
				}
				if asJSON || WantJSON() {
					printJSON(canary)
					return nil
				}
				fmt.Printf("✅ Canary fully promoted — %s/%s is now on v%s\n", author, name, canary.CanaryVersion)
				return nil
			}
			if percent <= 0 || percent >= 100 {
				return fmt.Errorf("--percent must be between 1 and 99 (use --full to complete the rollout)")
			}
			req := map[string]interface{}{"traffic_percent": percent}
			var canary CanaryConfig
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary", author, name)
			if err := client.Patch(path, req, &canary); err != nil {
				return fmt.Errorf("could not update canary: %w", err)
			}
			if asJSON || WantJSON() {
				printJSON(canary)
				return nil
			}
			fmt.Printf("🐤 Canary traffic updated to %d%% for %s/%s\n", canary.TrafficPercent, author, name)
			return nil
		},
	}
	cmd.Flags().IntVar(&percent, "percent", 0, "New traffic percentage (1–99)")
	cmd.Flags().BoolVar(&full, "full", false, "Fully promote canary to 100% (stable)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// ── rollback ─────────────────────────────────────────────────────────────────

func newCanaryRollbackCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "rollback [author/name]",
		Short:   "Roll back the canary to the stable version",
		Example: "  ff canary rollback\n  ff canary rollback alice/my-fn",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			var canary CanaryConfig
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary/rollback", author, name)
			if err := client.Post(path, map[string]interface{}{}, &canary); err != nil {
				return fmt.Errorf("could not roll back canary: %w", err)
			}
			if asJSON || WantJSON() {
				printJSON(canary)
				return nil
			}
			fmt.Printf("↩️  Canary rolled back — %s/%s is stable on v%s\n", author, name, canary.StableVersion)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// ── cancel ───────────────────────────────────────────────────────────────────

func newCanaryCancelCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "cancel [author/name]",
		Short:   "Cancel and remove the active canary deployment",
		Example: "  ff canary cancel\n  ff canary cancel --force",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			if !force && IsInteractive() {
				confirmed := PromptConfirm(fmt.Sprintf("Cancel canary for %s/%s?", author, name), false)
				if !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary", author, name)
			if err := client.Delete(path, nil); err != nil {
				return fmt.Errorf("could not cancel canary: %w", err)
			}
			fmt.Printf("🗑️  Canary cancelled for %s/%s\n", author, name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}

// ── history ──────────────────────────────────────────────────────────────────

func newCanaryHistoryCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "history [author/name]",
		Short:   "Show canary deployment history",
		Example: "  ff canary history\n  ff canary history alice/my-fn",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			author, name, err := resolveAuthorName(args)
			if err != nil {
				return err
			}
			client, err := NewAPIClient()
			if err != nil {
				return err
			}
			var history CanaryHistory
			path := fmt.Sprintf("/v1/registry/functions/%s/%s/canary/history", author, name)
			if err := client.Get(path, &history); err != nil {
				return fmt.Errorf("could not get canary history: %w", err)
			}
			if asJSON || WantJSON() {
				printJSON(history)
				return nil
			}
			if len(history.Configs) == 0 {
				fmt.Printf("No canary history for %s/%s\n", author, name)
				return nil
			}
			fmt.Printf("Canary history for %s/%s\n\n", author, name)
			fmt.Printf("  %-12s  %-12s  %-8s  %-10s  %s\n", "CANARY VER", "STABLE VER", "TRAFFIC", "STATUS", "STARTED")
			for _, c := range history.Configs {
				fmt.Printf("  %-12s  %-12s  %-8s  %-10s  %s\n",
					c.CanaryVersion,
					c.StableVersion,
					fmt.Sprintf("%d%%", c.TrafficPercent),
					c.Status,
					c.CreatedAt.Format("2006-01-02"),
				)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// ── helpers ──────────────────────────────────────────────────────────────────

func printCanaryStatus(c *CanaryConfig) {
	statusIcon := "🐤"
	if c.Status == "promoted" {
		statusIcon = "✅"
	} else if c.Status == "rolled_back" || c.Status == "cancelled" {
		statusIcon = "↩️ "
	}
	fmt.Printf("  Status        : %s %s\n", statusIcon, c.Status)
	fmt.Printf("  Canary version: %s\n", c.CanaryVersion)
	fmt.Printf("  Stable version: %s\n", c.StableVersion)
	fmt.Printf("  Traffic split : %d%% canary / %d%% stable\n", c.TrafficPercent, 100-c.TrafficPercent)
	if c.AutoPromote {
		fmt.Printf("  Auto-promote  : enabled (threshold %.1f%%, window %d min)\n", c.PromoteThreshold, c.PromoteWindow)
	}
}
