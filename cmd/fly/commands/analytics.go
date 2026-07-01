package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewAnalyticsCmd() *cobra.Command {
	var asJSON bool
	var period string
	var compare string
	var dimension string
	cmd := &cobra.Command{
		Use:     "analytics [author/name]",
		Aliases: []string{"metrics", "insights"},
		Short:   "Rich analytics beyond basic stats",
		Long: `Deep analytics for your functions — latency percentiles, error
breakdowns, geographic distribution, cost analysis, and trend
comparisons.

Goes beyond basic call counts with actionable insights.`,
		Example: `  ff analytics
  ff analytics alice/my-fn
  ff analytics --period 7d
  ff analytics --period 30d --compare previous
  ff analytics --dimension region
  ff analytics --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalytics(args, period, compare, dimension, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&period, "period", "7d", "Time period (1d, 7d, 30d, 90d)")
	cmd.Flags().StringVar(&compare, "compare", "", "Compare with previous period")
	cmd.Flags().StringVar(&dimension, "dimension", "", "Breakdown dimension (region, runtime, status, version)")
	return cmd
}

type AnalyticsPayload struct {
	Function   string           `json:"function"`
	Period     string           `json:"period"`
	Summary    AnalyticsSummary `json:"summary"`
	Latency    LatencyPercentiles `json:"latency"`
	Errors     []ErrorBreakdown `json:"errors,omitempty"`
	Geo        []GeoEntry       `json:"geo,omitempty"`
	Cost       CostBreakdown    `json:"cost,omitempty"`
	Trends     []TrendPoint     `json:"trends,omitempty"`
	Compare    *AnalyticsSummary `json:"compare,omitempty"`
	Insights   []string         `json:"insights,omitempty"`
}

type AnalyticsSummary struct {
	Invocations  int64   `json:"invocations"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	BandwidthMB  float64 `json:"bandwidth_mb,omitempty"`
}

type LatencyPercentiles struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
	Min  float64 `json:"min"`
}

type ErrorBreakdown struct {
	Code  int    `json:"code"`
	Count int64  `json:"count"`
	Pct   float64 `json:"pct"`
	Label string `json:"label,omitempty"`
}

type GeoEntry struct {
	Region     string  `json:"region"`
	Calls      int64   `json:"calls"`
	AvgLatency float64 `json:"avg_latency_ms"`
	Pct        float64 `json:"pct"`
}

type CostBreakdown struct {
	Compute    float64 `json:"compute"`
	Bandwidth  float64 `json:"bandwidth"`
	Storage    float64 `json:"storage"`
	Total      float64 `json:"total"`
	Currency   string  `json:"currency"`
}

type TrendPoint struct {
	Timestamp  string  `json:"timestamp"`
	Invocations int64  `json:"invocations"`
	Errors     int64   `json:"errors"`
	AvgLatency float64 `json:"avg_latency_ms"`
}

func runAnalytics(args []string, period, compare, dimension string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var payload AnalyticsPayload
	payload.Function = author + "/" + name
	payload.Period = period

	basePath := fmt.Sprintf("/v1/registry/functions/%s/%s/analytics?period=%s", author, name, period)
	if dimension != "" {
		basePath += "&dimension=" + dimension
	}

	if err := client.Get(basePath, &payload); err != nil {
		return fmt.Errorf("could not fetch analytics: %w", err)
	}

	if compare != "" {
		comparePath := fmt.Sprintf("/v1/registry/functions/%s/%s/analytics?period=%s&compare=true", author, name, compare)
		var prev AnalyticsSummary
		if err := client.Get(comparePath, &prev); err == nil {
			payload.Compare = &prev
		}
	}

	if asJSON || WantJSON() {
		printJSON(payload)
		return nil
	}

	printAnalytics(payload)
	return nil
}

func printAnalytics(a AnalyticsPayload) {
	s := a.Summary

	fmt.Printf("\n📊 Analytics: %s (%s)\n", a.Function, a.Period)
	fmt.Println(strings.Repeat("─", 60))

	// Summary
	fmt.Printf("\n  Invocations:   %s\n", formatNumber(s.Invocations))
	fmt.Printf("  Success rate:  %.2f%%\n", s.SuccessRate*100)
	fmt.Printf("  Avg latency:   %.0f ms\n", s.AvgLatencyMs)
	fmt.Printf("  P99 latency:   %.0f ms\n", s.P99LatencyMs)
	if s.CostUSD > 0 {
		fmt.Printf("  Cost:          $%.4f\n", s.CostUSD)
	}
	if s.BandwidthMB > 0 {
		fmt.Printf("  Bandwidth:     %.1f MB\n", s.BandwidthMB)
	}

	// Comparison
	if a.Compare != nil {
		c := a.Compare
		fmt.Printf("\n  vs previous period:\n")
		fmt.Printf("    Invocations:  %s → %s (%s)\n",
			formatNumber(c.Invocations), formatNumber(s.Invocations),
			deltaStr(s.Invocations-c.Invocations))
		fmt.Printf("    Success rate: %.2f%% → %.2f%%\n", c.SuccessRate*100, s.SuccessRate*100)
		fmt.Printf("    Avg latency:  %.0f ms → %.0f ms\n", c.AvgLatencyMs, s.AvgLatencyMs)
		if c.CostUSD > 0 || s.CostUSD > 0 {
			fmt.Printf("    Cost:         $%.4f → $%.4f\n", c.CostUSD, s.CostUSD)
		}
	}

	// Latency percentiles
	if a.Latency.P50 > 0 || a.Latency.P99 > 0 {
		l := a.Latency
		fmt.Printf("\n  ⏱️  Latency Distribution\n")
		fmt.Printf("    P50:  %.0f ms\n", l.P50)
		fmt.Printf("    P90:  %.0f ms\n", l.P90)
		fmt.Printf("    P95:  %.0f ms\n", l.P95)
		fmt.Printf("    P99:  %.0f ms\n", l.P99)
		if l.Max > 0 {
			fmt.Printf("    Max:  %.0f ms\n", l.Max)
		}
		if l.Min > 0 {
			fmt.Printf("    Min:  %.0f ms\n", l.Min)
		}
	}

	// Error breakdown
	if len(a.Errors) > 0 {
		fmt.Printf("\n  ❌ Error Breakdown\n")
		for _, e := range a.Errors {
			label := e.Label
			if label == "" {
				label = fmt.Sprintf("HTTP %d", e.Code)
			}
			bar := strings.Repeat("█", int(e.Pct/5))
			fmt.Printf("    %-14s %s %d (%.1f%%)\n", label, bar, e.Count, e.Pct)
		}
	}

	// Geographic distribution
	if len(a.Geo) > 0 {
		fmt.Printf("\n  🌍 Geographic Distribution\n")
		for _, g := range a.Geo {
			fmt.Printf("    %-12s  %s calls  %.0f ms avg  %.1f%%\n",
				g.Region, formatNumber(g.Calls), g.AvgLatency, g.Pct)
		}
	}

	// Cost breakdown
	if a.Cost.Total > 0 {
		c := a.Cost
		fmt.Printf("\n  💰 Cost Breakdown (%s)\n", c.Currency)
		fmt.Printf("    Compute:    $%.4f\n", c.Compute)
		fmt.Printf("    Bandwidth:  $%.4f\n", c.Bandwidth)
		if c.Storage > 0 {
			fmt.Printf("    Storage:    $%.4f\n", c.Storage)
		}
		fmt.Printf("    ─────────────────\n")
		fmt.Printf("    Total:      $%.4f\n", c.Total)
	}

	// Trends
	if len(a.Trends) > 0 {
		fmt.Printf("\n  📈 Trend (%d points)\n", len(a.Trends))
		maxInv := int64(0)
		for _, t := range a.Trends {
			if t.Invocations > maxInv {
				maxInv = t.Invocations
			}
		}
		for _, t := range a.Trends {
			barLen := 0
			if maxInv > 0 {
				barLen = int(float64(t.Invocations) / float64(maxInv) * 20)
			}
			bar := strings.Repeat("█", barLen) + strings.Repeat("░", 20-barLen)
			ts := t.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s  %s  %s\n", ts, bar, formatNumber(t.Invocations))
		}
	}

	// Insights
	if len(a.Insights) > 0 {
		fmt.Printf("\n  💡 Insights\n")
		for _, insight := range a.Insights {
			fmt.Printf("    • %s\n", insight)
		}
	}

	fmt.Println()
}

func deltaStr(delta int64) string {
	if delta > 0 {
		return fmt.Sprintf("+%s", formatNumber(delta))
	}
	if delta < 0 {
		return formatNumber(delta)
	}
	return "unchanged"
}
