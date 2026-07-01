package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewExecHistoryCmd() *cobra.Command {
	var asJSON bool
	var function string
	var limit int
	var status string
	cmd := &cobra.Command{
		Use:     "exec-history [id]",
		Aliases: []string{"exec", "history"},
		Short:   "View past function executions",
		Long: `View execution history for your functions.

Without an argument, lists recent executions across all functions. Pass an
execution ID to see full details for a single run including input, output,
timing, and error information.`,
		Example: `  ff exec-history
  ff exec-history <execution-id>
  ff exec-history --function alice/my-fn
  ff exec-history --function alice/my-fn --limit 10
  ff exec-history --status error
  ff exec-history <id> --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runExecHistoryGet(args[0], asJSON)
			}
			return runExecHistoryList(function, status, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVarP(&function, "function", "f", "", "Filter by function (author/name)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 25, "Number of executions to show (1-100)")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (success, error, timeout)")
	return cmd
}

type Execution struct {
	ID          string `json:"id"`
	FunctionID  string `json:"function_id,omitempty"`
	Author      string `json:"author,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Status      string `json:"status"`
	StatusCode  int    `json:"status_code,omitempty"`
	Method      string `json:"method,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	Region      string `json:"region,omitempty"`
	Cached      bool   `json:"cached,omitempty"`
	Error       string `json:"error,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type ExecutionDetail struct {
	Execution
	Input       interface{}            `json:"input,omitempty"`
	Output      interface{}            `json:"output,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	DurationMs  int64                  `json:"duration_ms,omitempty"`
	MemoryMB    int                    `json:"memory_mb,omitempty"`
}

type ExecutionListResponse struct {
	Executions []Execution `json:"executions"`
	Total      int         `json:"total,omitempty"`
}

func runExecHistoryList(function, status string, limit int, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	params := []string{fmt.Sprintf("limit=%d", limit)}
	if function != "" {
		params = append(params, "function="+function)
	}
	if status != "" {
		params = append(params, "status="+status)
	}

	path := "/v1/executions?" + strings.Join(params, "&")
	var resp ExecutionListResponse
	if err := client.Get(path, &resp); err != nil {
		return fmt.Errorf("could not fetch execution history: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Executions) == 0 {
		fmt.Println("No executions found.")
		if function != "" {
			fmt.Printf("\nThe function %s may not have been invoked yet.\n", function)
		}
		return nil
	}

	total := resp.Total
	if total == 0 {
		total = len(resp.Executions)
	}
	fmt.Printf("\nExecutions (%d)\n\n", total)
	fmt.Printf("  %-12s  %-28s  %-8s  %-8s  %-8s  %-10s  %s\n",
		"ID", "FUNCTION", "STATUS", "CODE", "LATENCY", "REGION", "TIME")
	fmt.Println("  " + strings.Repeat("-", 100))

	for _, ex := range resp.Executions {
		id := ex.ID
		if len(id) > 10 {
			id = id[:10]
		}
		fn := ex.Author + "/" + ex.Name
		if fn == "/" {
			fn = ex.FunctionID
			if len(fn) > 26 {
				fn = fn[:26] + ".."
			}
		}
		statusIcon := statusEmoji(ex.Status)
		code := "-"
		if ex.StatusCode > 0 {
			code = fmt.Sprintf("%d", ex.StatusCode)
		}
		latency := "-"
		if ex.LatencyMs > 0 {
			latency = fmt.Sprintf("%dms", ex.LatencyMs)
		}
		region := ex.Region
		if region == "" {
			region = "-"
		}
		ts := formatExecTime(ex.StartedAt)
		if ts == "" {
			ts = formatExecTime(ex.CreatedAt)
		}

		fmt.Printf("  %-12s  %-28s  %s%-6s  %-8s  %-8s  %-10s  %s\n",
			id, fn, statusIcon, ex.Status, code, latency, region, ts)
	}

	fmt.Println()
	return nil
}

func runExecHistoryGet(id string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var detail ExecutionDetail
	if err := client.Get("/v1/executions/"+id, &detail); err != nil {
		return fmt.Errorf("could not fetch execution %s: %w", id, err)
	}

	if asJSON || WantJSON() {
		printJSON(detail)
		return nil
	}

	fn := detail.Author + "/" + detail.Name
	if fn == "/" {
		fn = detail.FunctionID
	}
	statusIcon := statusEmoji(detail.Status)

	fmt.Printf("\nExecution %s\n\n", detail.ID)
	fmt.Printf("  Status:     %s %s", statusIcon, detail.Status)
	if detail.StatusCode > 0 {
		fmt.Printf(" (HTTP %d)", detail.StatusCode)
	}
	fmt.Println()
	if fn != "" {
		fmt.Printf("  Function:   %s", fn)
		if detail.Version != "" {
			fmt.Printf(" @ %s", detail.Version)
		}
		fmt.Println()
	}
	if detail.Method != "" {
		fmt.Printf("  Method:     %s\n", detail.Method)
	}
	if detail.LatencyMs > 0 {
		fmt.Printf("  Latency:    %dms\n", detail.LatencyMs)
	}
	if detail.DurationMs > 0 && detail.DurationMs != detail.LatencyMs {
		fmt.Printf("  Duration:   %dms\n", detail.DurationMs)
	}
	if detail.Region != "" {
		fmt.Printf("  Region:     %s\n", detail.Region)
	}
	if detail.Cached {
		fmt.Printf("  Cached:     yes\n")
	}
	if detail.MemoryMB > 0 {
		fmt.Printf("  Memory:     %d MB\n", detail.MemoryMB)
	}
	if detail.StartedAt != "" {
		fmt.Printf("  Started:    %s\n", detail.StartedAt)
	}
	if detail.FinishedAt != "" {
		fmt.Printf("  Finished:   %s\n", detail.FinishedAt)
	}

	if detail.Error != "" {
		fmt.Printf("\n  Error:\n    %s\n", detail.Error)
	}

	if detail.Input != nil {
		fmt.Printf("\n  Input:\n")
		printIndented(detail.Input)
	}
	if detail.Output != nil {
		fmt.Printf("\n  Output:\n")
		printIndented(detail.Output)
	}

	fmt.Println()
	return nil
}

func statusEmoji(s string) string {
	switch strings.ToLower(s) {
	case "success", "ok":
		return "✅"
	case "error", "failed":
		return "❌"
	case "timeout":
		return "⏱️"
	case "running":
		return "🔄"
	default:
		return "⚪"
	}
}

func formatExecTime(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		if len(ts) > 19 {
			return ts[:19]
		}
		return ts
	}
	if time.Since(t) < 24*time.Hour {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 02 15:04")
}

func printIndented(v interface{}) {
	data, ok := v.([]byte)
	if !ok {
		printJSON(v)
		return
	}
	fmt.Printf("    %s\n", string(data))
}
