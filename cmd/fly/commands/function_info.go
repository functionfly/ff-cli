package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newFunctionInfoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info [author/name]",
		Short: "Show detailed function information",
		Long: `Display an aggregated view of a deployed function: metadata, active
version, version history, usage stats, trust score, and reviews.

This is a single-pane view combining data from multiple API endpoints.`,
		Example: `  ff function info
  ff function info alice/my-fn
  ff function info alice/my-fn --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionInfo(args, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

type FunctionMetadata struct {
	ID            string            `json:"id"`
	Author        string            `json:"author"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Runtime       string            `json:"runtime"`
	Public        bool              `json:"public"`
	Description   string            `json:"description,omitempty"`
	Deterministic bool              `json:"deterministic"`
	CacheTTL      int               `json:"cache_ttl,omitempty"`
	TimeoutMS     int               `json:"timeout_ms,omitempty"`
	MemoryMB      int               `json:"memory_mb,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	CreatedAt     string            `json:"created_at,omitempty"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
}

type TrustScore struct {
	Score       float64           `json:"score"`
	Grade       string            `json:"grade,omitempty"`
	Factors     map[string]float64 `json:"factors,omitempty"`
	LastChecked string            `json:"last_checked,omitempty"`
}

type ReviewSummary struct {
	Average   float64 `json:"average"`
	Count     int     `json:"count"`
	FiveStar  int     `json:"five_star,omitempty"`
	FourStar  int     `json:"four_star,omitempty"`
	ThreeStar int     `json:"three_star,omitempty"`
	TwoStar   int     `json:"two_star,omitempty"`
	OneStar   int     `json:"one_star,omitempty"`
}

type FunctionInfo struct {
	Function FunctionMetadata `json:"function"`
	Versions []VersionInfo    `json:"versions,omitempty"`
	Stats    *StatsResponse   `json:"stats,omitempty"`
	Trust    *TrustScore      `json:"trust,omitempty"`
	Reviews  *ReviewSummary   `json:"reviews,omitempty"`
}

func runFunctionInfo(args []string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	info := FunctionInfo{}

	// 1. Function metadata
	var fn FunctionMetadata
	fnPath := fmt.Sprintf("/v1/registry/functions/%s/%s", author, name)
	if err := client.Get(fnPath, &fn); err != nil {
		return fmt.Errorf("could not fetch function info: %w", err)
	}
	info.Function = fn

	// 2. Versions (non-fatal)
	var versions []VersionInfo
	verPath := fmt.Sprintf("/v1/registry/functions/%s/%s/versions", author, name)
	if err := client.Get(verPath, &versions); err == nil {
		info.Versions = versions
	}

	// 3. Stats (non-fatal)
	var stats StatsResponse
	statsPath := fmt.Sprintf("/v1/registry/functions/%s/%s/stats?period=30d", author, name)
	if err := client.Get(statsPath, &stats); err == nil {
		info.Stats = &stats
	}

	// 4. Trust score (non-fatal)
	var trust TrustScore
	trustPath := fmt.Sprintf("/v1/registry/functions/%s/%s/trust", author, name)
	if err := client.Get(trustPath, &trust); err == nil {
		info.Trust = &trust
	}

	// 5. Reviews (non-fatal)
	var reviews ReviewSummary
	reviewsPath := fmt.Sprintf("/v1/registry/functions/%s/%s/reviews", author, name)
	if err := client.Get(reviewsPath, &reviews); err == nil {
		info.Reviews = &reviews
	}

	if asJSON || WantJSON() {
		printJSON(info)
		return nil
	}

	printFunctionInfo(info)
	return nil
}

func printFunctionInfo(info FunctionInfo) {
	fn := info.Function
	fmt.Printf("\n📦 %s/%s\n", fn.Author, fn.Name)
	fmt.Println(strings.Repeat("─", 60))

	// Metadata
	fmt.Printf("  ID:            %s\n", fn.ID)
	fmt.Printf("  Version:       %s\n", fn.Version)
	fmt.Printf("  Runtime:       %s\n", fn.Runtime)
	fmt.Printf("  Visibility:    %s\n", visibilityLabel(fn.Public))
	if fn.Description != "" {
		fmt.Printf("  Description:   %s\n", fn.Description)
	}
	if fn.Deterministic {
		fmt.Printf("  Deterministic: yes\n")
	}
	if fn.TimeoutMS > 0 {
		fmt.Printf("  Timeout:       %d ms\n", fn.TimeoutMS)
	}
	if fn.MemoryMB > 0 {
		fmt.Printf("  Memory:        %d MB\n", fn.MemoryMB)
	}
	if fn.CacheTTL > 0 {
		fmt.Printf("  Cache TTL:     %d s\n", fn.CacheTTL)
	}
	if fn.UpdatedAt != "" {
		fmt.Printf("  Updated:       %s\n", fn.UpdatedAt)
	}

	// Trust score
	if info.Trust != nil {
		fmt.Printf("\n🔒 Trust Score\n")
		grade := info.Trust.Grade
		if grade == "" {
			grade = scoreToGrade(info.Trust.Score)
		}
		fmt.Printf("  Score: %.1f/100 (%s)\n", info.Trust.Score, grade)
		if len(info.Trust.Factors) > 0 {
			for k, v := range info.Trust.Factors {
				fmt.Printf("    %-16s %.1f\n", k+":", v)
			}
		}
	}

	// Stats
	if info.Stats != nil {
		s := info.Stats
		fmt.Printf("\n📊 Stats (30d)\n")
		fmt.Printf("  Calls:        %s\n", formatNumber(s.TotalCalls))
		fmt.Printf("  Success rate: %.2f%%\n", s.SuccessRate*100)
		fmt.Printf("  Avg latency:  %.0f ms\n", s.AvgLatencyMs)
		if s.Revenue > 0 {
			fmt.Printf("  Revenue:      $%.2f\n", s.Revenue)
		}
	}

	// Reviews
	if info.Reviews != nil && info.Reviews.Count > 0 {
		r := info.Reviews
		fmt.Printf("\n⭐ Reviews\n")
		fmt.Printf("  Average: %.1f/5 (%d reviews)\n", r.Average, r.Count)
		if r.FiveStar > 0 || r.FourStar > 0 {
			fmt.Printf("  5★ %d  4★ %d  3★ %d  2★ %d  1★ %d\n",
				r.FiveStar, r.FourStar, r.ThreeStar, r.TwoStar, r.OneStar)
		}
	}

	// Versions
	if len(info.Versions) > 0 {
		fmt.Printf("\n📋 Versions (%d)\n", len(info.Versions))
		for _, v := range info.Versions {
			active := ""
			if v.Active {
				active = " ← active"
			}
			hash := v.Hash
			if len(hash) > 8 {
				hash = hash[:8]
			}
			deployed := v.DeployedAt
			if len(deployed) > 10 {
				deployed = deployed[:10]
			}
			fmt.Printf("  v%-10s  %s  %s%s\n", v.Version, hash, deployed, active)
		}
	}

	fmt.Println()
}

func visibilityLabel(public bool) string {
	if public {
		return "public"
	}
	return "private"
}

func scoreToGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}
