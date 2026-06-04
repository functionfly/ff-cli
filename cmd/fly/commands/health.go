package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// HealthStatsResponse is the subset of the stats API response used for health.
type HealthStatsResponse struct {
	FunctionID   string  `json:"function_id"`
	Author       string  `json:"author"`
	Name         string  `json:"name"`
	TotalCalls   int64   `json:"total_calls"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	OverallScore float64 `json:"overall_score"`
}

func NewHealthCmd() *cobra.Command {
	var asJSON bool
	var watch bool
	cmd := &cobra.Command{
		Use:   "health [author/name]",
		Short: "Check the health of your deployed function",
		Long: `Fetches live stats for the deployed function and reports availability,
latency, and success rate. Reads function name from functionfly.jsonc when
no argument is given.`,
		Example: "  ff health\n  ff health alice/my-fn\n  ff health --watch\n  ff health --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealth(args, asJSON, watch)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Refresh every 30 s until interrupted")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runHealth(args []string, asJSON, watch bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	if watch {
		fmt.Printf("Watching health for %s/%s — Ctrl+C to stop\n\n", author, name)
		for {
			if err := printHealth(client, author, name, asJSON); err != nil {
				fmt.Printf("error: %v\n", err)
			}
			time.Sleep(30 * time.Second)
		}
	}
	return printHealth(client, author, name, asJSON)
}

func printHealth(client *APIClient, author, name string, asJSON bool) error {
	path := fmt.Sprintf("/v1/registry/functions/%s/%s/stats", author, name)
	var stats HealthStatsResponse
	if err := client.Get(path, &stats); err != nil {
		return fmt.Errorf("could not fetch stats for %s/%s: %w", author, name, err)
	}

	if asJSON || WantJSON() {
		fmt.Printf(`{"function":"%s/%s","success_rate":%.4f,"avg_latency_ms":%.1f,"p95_latency_ms":%.1f,"total_calls":%d,"overall_score":%.2f,"checked_at":"%s"}`,
			author, name,
			stats.SuccessRate,
			stats.AvgLatencyMs,
			stats.P95LatencyMs,
			stats.TotalCalls,
			stats.OverallScore,
			time.Now().UTC().Format(time.RFC3339),
		)
		fmt.Println()
		return nil
	}

	status := "✅ healthy"
	if stats.SuccessRate < 0.95 {
		status = "⚠️  degraded"
	}
	if stats.SuccessRate < 0.80 {
		status = "❌ unhealthy"
	}

	fmt.Printf("Health: %s/%s  %s\n", author, name, status)
	fmt.Printf("  Success rate  : %.2f%%\n", stats.SuccessRate*100)
	fmt.Printf("  Avg latency   : %.1f ms\n", stats.AvgLatencyMs)
	fmt.Printf("  p95 latency   : %.1f ms\n", stats.P95LatencyMs)
	fmt.Printf("  Total calls   : %d (last 24 h)\n", stats.TotalCalls)
	fmt.Printf("  Overall score : %.2f\n", stats.OverallScore)
	return nil
}
