package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewTimeMachineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "time-machine",
		Aliases: []string{"timemachine", "tm"},
		Short:   "Replay and inspect past function states",
		Long: `Travel back in time to inspect, compare, and replay past function
deployments.

Create replays of previous versions, view diffs between any two points
in time, and restore function state from history. Think of it as
git log + git diff + git checkout for your deployed functions.`,
		Example: `  ff time-machine list
  ff time-machine list alice/my-fn
  ff time-machine replay <version>
  ff time-machine diff v1.0.0 v1.1.0
  ff time-machine inspect v1.0.0
  ff time-machine list --json`,
	}
	cmd.AddCommand(newTimeMachineListCmd())
	cmd.AddCommand(newTimeMachineReplayCmd())
	cmd.AddCommand(newTimeMachineDiffCmd())
	cmd.AddCommand(newTimeMachineInspectCmd())
	return cmd
}

func TimeMachineCmd() *cobra.Command {
	return NewTimeMachineCmd()
}

type TimeSnapshot struct {
	Version   string `json:"version"`
	Hash      string `json:"hash"`
	Env       string `json:"env,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Message   string `json:"message,omitempty"`
	Size      int    `json:"size,omitempty"`
	Active    bool   `json:"active,omitempty"`
}

type TimeListResponse struct {
	Snapshots []TimeSnapshot `json:"snapshots"`
	Total     int            `json:"total,omitempty"`
}

type TimeDiff struct {
	From       TimeSnapshot   `json:"from"`
	To         TimeSnapshot   `json:"to"`
	Changes    []TimeChange   `json:"changes"`
	Summary    string         `json:"summary,omitempty"`
}

type TimeChange struct {
	Field   string      `json:"field"`
	Before  interface{} `json:"before,omitempty"`
	After   interface{} `json:"after,omitempty"`
	Kind    string      `json:"kind"`
}

// --- list ---

func newTimeMachineListCmd() *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:     "list [author/name]",
		Aliases: []string{"ls", "log"},
		Short:   "List function snapshots",
		Long:    `List all historical snapshots (versions) of a function.`,
		Example: `  ff time-machine list
  ff time-machine list alice/my-fn
  ff time-machine list --limit 20
  ff time-machine list --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTimeMachineList(args, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum snapshots to show")
	return cmd
}

func runTimeMachineList(args []string, limit int, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var resp TimeListResponse
	path := fmt.Sprintf("/v1/registry/functions/%s/%s/versions?limit=%d", author, name, limit)
	if err := client.Get(path, &resp); err != nil {
		// Fall back to versions endpoint returning VersionInfo
		var versions []VersionInfo
		if err2 := client.Get(path, &versions); err2 != nil {
			return fmt.Errorf("could not fetch snapshots: %w", err)
		}
		for _, v := range versions {
			resp.Snapshots = append(resp.Snapshots, TimeSnapshot{
				Version:   v.Version,
				Hash:      v.Hash,
				CreatedAt: v.DeployedAt,
				Active:    v.Active,
			})
		}
		resp.Total = len(resp.Snapshots)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Snapshots) == 0 {
		fmt.Printf("No snapshots for %s/%s.\n", author, name)
		return nil
	}

	fmt.Printf("\n⏰ Time Machine: %s/%s (%d snapshots)\n\n", author, name, len(resp.Snapshots))

	for _, s := range resp.Snapshots {
		hash := s.Hash
		if len(hash) > 10 {
			hash = hash[:10]
		}
		if hash == "" {
			hash = "-"
		}
		ts := s.CreatedAt
		if len(ts) > 19 {
			ts = ts[:19]
		}
		if ts == "" {
			ts = "-"
		}
		active := ""
		if s.Active {
			active = " ← current"
		}
		env := ""
		if s.Env != "" {
			env = fmt.Sprintf(" [%s]", s.Env)
		}
		msg := ""
		if s.Message != "" {
			msg = fmt.Sprintf("  %s", s.Message)
		}
		fmt.Printf("  v%-12s  %s  %s%s%s%s\n", s.Version, hash, ts, env, active, msg)
	}

	fmt.Println()
	return nil
}

// --- replay ---

func newTimeMachineReplayCmd() *cobra.Command {
	var asJSON bool
	var force bool
	cmd := &cobra.Command{
		Use:   "replay <version> [author/name]",
		Short: "Replay a previous version",
		Long: `Replay a previous function version by re-deploying it as the current
version. This creates a new snapshot based on the old code.

This is equivalent to 'git checkout' for your function — the old code
becomes the new active version.`,
		Example: `  ff time-machine replay 1.0.0
  ff time-machine replay 1.0.0 alice/my-fn
  ff time-machine replay 1.0.0 --force --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTimeMachineReplay(args[0], args[1:], force, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func runTimeMachineReplay(version string, tail []string, force, asJSON bool) error {
	author, name, err := resolveAuthorName(tail)
	if err != nil {
		return err
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Replay %s/%s v%s? This re-deploys the old code as current.", author, name, version),
			false,
		)
		if !confirmed {
			fmt.Println("Replay cancelled.")
			return nil
		}
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/registry/functions/%s/%s/versions/%s/replay", author, name, version)
	var result map[string]interface{}
	if err := client.Post(path, nil, &result); err != nil {
		return fmt.Errorf("could not replay v%s: %w", version, err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"function": author + "/" + name,
			"replayed": version,
			"result":   result,
		})
		return nil
	}

	fmt.Printf("✅ Replayed %s/%s v%s\n", author, name, version)
	fmt.Printf("   Old code is now the active version.\n")
	fmt.Printf("   Run 'ff test' to verify.\n")
	return nil
}

// --- diff ---

func newTimeMachineDiffCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "diff <from> <to> [author/name]",
		Short: "Diff two function snapshots",
		Long: `Compare two historical versions of a function and show what changed.

Accepts version numbers (1.0.0, 1.1.0) or snapshot hashes. Shows
field-level changes between the two versions.`,
		Example: `  ff time-machine diff 1.0.0 1.1.0
  ff time-machine diff 1.0.0 1.1.0 alice/my-fn
  ff time-machine diff v1.0.0 v1.1.0 --json`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTimeMachineDiff(args[0], args[1], args[2:], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runTimeMachineDiff(from, to string, tail []string, asJSON bool) error {
	author, name, err := resolveAuthorName(tail)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var diff TimeDiff
	path := fmt.Sprintf("/v1/registry/functions/%s/%s/diff?from=%s&to=%s", author, name, from, to)
	if err := client.Get(path, &diff); err != nil {
		return fmt.Errorf("could not diff snapshots: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(diff)
		return nil
	}

	fmt.Printf("\n⏪ Time Machine Diff: %s/%s\n", author, name)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  From: v%s\n", from)
	fmt.Printf("  To:   v%s\n", to)

	if len(diff.Changes) == 0 {
		fmt.Printf("\n  ✅ No differences — versions are identical.\n\n")
		return nil
	}

	if diff.Summary != "" {
		fmt.Printf("\n  Summary: %s\n", diff.Summary)
	}

	fmt.Printf("\n  %-20s  %-20s  %s\n", "FIELD", "BEFORE", "AFTER")
	fmt.Println("  " + strings.Repeat("─", 56))

	for _, c := range diff.Changes {
		before := formatDiffValue(c.Before)
		after := formatDiffValue(c.After)
		kindIcon := ""
		switch c.Kind {
		case "added":
			kindIcon = "+"
		case "removed":
			kindIcon = "-"
		case "changed":
			kindIcon = "~"
		}
		fmt.Printf("  %s %-19s  %-20s  %s\n", kindIcon, c.Field, before, after)
	}

	fmt.Printf("\n  %d change(s) between v%s and v%s\n\n", len(diff.Changes), from, to)
	return nil
}

// --- inspect ---

func newTimeMachineInspectCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "inspect <version> [author/name]",
		Short: "Inspect a specific snapshot",
		Long:  `View full details of a specific historical snapshot.`,
		Example: `  ff time-machine inspect 1.0.0
  ff time-machine inspect 1.0.0 alice/my-fn
  ff time-machine inspect 1.0.0 --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTimeMachineInspect(args[0], args[1:], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runTimeMachineInspect(version string, tail []string, asJSON bool) error {
	author, name, err := resolveAuthorName(tail)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var snapshot TimeSnapshot
	path := fmt.Sprintf("/v1/registry/functions/%s/%s/versions/%s", author, name, version)
	if err := client.Get(path, &snapshot); err != nil {
		return fmt.Errorf("could not inspect v%s: %w", version, err)
	}

	if asJSON || WantJSON() {
		printJSON(snapshot)
		return nil
	}

	fmt.Printf("\n🔍 Snapshot: %s/%s v%s\n", author, name, version)
	fmt.Println(strings.Repeat("─", 55))
	fmt.Printf("  Version:   %s\n", snapshot.Version)
	if snapshot.Hash != "" {
		fmt.Printf("  Hash:      %s\n", snapshot.Hash)
	}
	if snapshot.Env != "" {
		fmt.Printf("  Env:       %s\n", snapshot.Env)
	}
	if snapshot.Size > 0 {
		fmt.Printf("  Size:      %d bytes\n", snapshot.Size)
	}
	if snapshot.Message != "" {
		fmt.Printf("  Message:   %s\n", snapshot.Message)
	}
	if snapshot.CreatedAt != "" {
		fmt.Printf("  Created:   %s\n", snapshot.CreatedAt)
	}
	if snapshot.Active {
		fmt.Printf("  Status:    ✅ current\n")
	}
	fmt.Println()
	return nil
}

func formatDiffValue(v interface{}) string {
	if v == nil {
		return "-"
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > 18 {
		return s[:15] + "..."
	}
	if s == "" {
		return "(empty)"
	}
	return s
}
