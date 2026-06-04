package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func NewLogsCmd() *cobra.Command {
	var follow bool
	var tail int
	var since string
	var asJSON bool
	var level string
	cmd := &cobra.Command{
		Use:     "logs [author/name]",
		Short:   "Stream live execution logs",
		Example: "  ff logs\n  ff logs alice/my-fn\n  ff logs --follow\n  ff logs --tail 100\n  ff logs --level error\n  ff logs --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(args, follow, tail, since, level, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs in real-time")
	cmd.Flags().IntVar(&tail, "tail", 50, "Number of recent log lines to show")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since duration (e.g. 1h, 30m)")
	cmd.Flags().StringVar(&level, "level", "", "Filter by log level (info, warn, error)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output logs as JSON")
	return cmd
}

type LogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Level      string    `json:"level"`
	Message    string    `json:"message"`
	RequestID  string    `json:"request_id,omitempty"`
	LatencyMs  int64     `json:"latency_ms,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
	Region     string    `json:"region,omitempty"`
	Cached     bool      `json:"cached,omitempty"`
}

func runLogs(args []string, follow bool, tail int, since, level string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	if !asJSON && !WantJSON() {
		fmt.Printf("📋 Logs for %s/%s\n", author, name)
		if follow {
			fmt.Printf("   Streaming... (Ctrl+C to stop)\n")
		}
		fmt.Println()
	}
	params := []string{fmt.Sprintf("tail=%d", tail)}
	if since != "" {
		params = append(params, "since="+since)
	}
	if level != "" {
		params = append(params, "level="+level)
	}
	path := fmt.Sprintf("/v1/registry/%s/%s/logs?%s", author, name, strings.Join(params, "&"))
	if follow {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		done := make(chan struct{})
		var streamErr error
		go func() {
			defer close(done)
			streamErr = client.StreamLines(path+"&stream=true", func(line string) bool {
				select {
				case <-sigCh:
					return false
				default:
				}
				if line == "" || strings.HasPrefix(line, ":") {
					return true
				}
				data := strings.TrimPrefix(line, "data: ")
				var entry LogEntry
				if jsonErr := json.Unmarshal([]byte(data), &entry); jsonErr != nil {
					fmt.Println(data)
					return true
				}
				printLogEntry(entry, asJSON)
				return true
			})
		}()
		select {
		case <-sigCh:
		case <-done:
		}
		fmt.Printf("\n🛑 Stopped streaming logs\n")
		return streamErr
	}
	var logs []LogEntry
	if err := client.Get(path, &logs); err != nil {
		return fmt.Errorf("could not fetch logs: %w", err)
	}
	if asJSON {
		data, _ := json.MarshalIndent(logs, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	if len(logs) == 0 {
		fmt.Println("No logs found.")
		fmt.Println("   → Your function may not have been invoked yet")
		return nil
	}
	for _, entry := range logs {
		printLogEntry(entry, false)
	}
	return nil
}

func printLogEntry(entry LogEntry, asJSON bool) {
	if asJSON {
		data, _ := json.Marshal(entry)
		fmt.Println(string(data))
		return
	}
	ts := entry.Timestamp.Format("15:04:05")
	level := strings.ToUpper(entry.Level)
	useColor := IsColorTerminal()
	levelColor := ""
	reset := ""
	if useColor {
		reset = "\033[0m"
		switch strings.ToLower(entry.Level) {
		case "error":
			levelColor = "\033[31m"
		case "warn", "warning":
			levelColor = "\033[33m"
		case "info":
			levelColor = "\033[36m"
		}
	}
	extras := ""
	if entry.LatencyMs > 0 {
		extras += fmt.Sprintf(" %dms", entry.LatencyMs)
	}
	if entry.StatusCode > 0 {
		extras += fmt.Sprintf(" HTTP/%d", entry.StatusCode)
	}
	if entry.Region != "" {
		extras += fmt.Sprintf(" [%s]", entry.Region)
	}
	if entry.Cached {
		extras += " (cached)"
	}
	fmt.Printf("%s %s%-5s%s %s%s\n", ts, levelColor, level, reset, entry.Message, extras)
}
