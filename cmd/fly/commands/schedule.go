package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// SchedulePreset represents a schedule preset
type SchedulePreset struct {
	Name        string `json:"name"`
	Cron        string `json:"cron"`
	Description string `json:"description"`
}

// ScheduleInfo represents schedule information
type ScheduleInfo struct {
	FunctionID  string    `json:"function_id"`
	Cron        string    `json:"cron"`
	Timezone    string    `json:"timezone"`
	Enabled     bool      `json:"enabled"`
	LastRun     time.Time `json:"last_run"`
	NextRun     time.Time `json:"next_run"`
	Description string    `json:"description"`
}

func NewScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage scheduled function executions",
		Example: `  ff schedule set "*/5 * * * *"          # Run every 5 minutes
  ff schedule set --preset "every-hour"  # Use a preset
  ff schedule list                       # List all schedules
  ff schedule get                        # Get schedule for current function
  ff schedule remove                     # Remove schedule
  ff schedule presets                    # List available presets`,
	}
	cmd.AddCommand(
		newScheduleSetCmd(),
		newScheduleListCmd(),
		newScheduleGetCmd(),
		newScheduleRemoveCmd(),
		newSchedulePresetsCmd(),
		newScheduleTriggerCmd(),
	)
	return cmd
}

func newScheduleSetCmd() *cobra.Command {
	var preset string
	var timezone string
	var runOnDeploy bool
	cmd := &cobra.Command{
		Use:   "set <cron-expression>",
		Short: "Set a schedule for the current function",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cronExpr := ""
			if len(args) > 0 {
				cronExpr = args[0]
			}
			return runScheduleSet(cronExpr, preset, timezone, runOnDeploy)
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "", "Use a preset (every-minute, every-hour, every-day, etc.)")
	cmd.Flags().StringVar(&timezone, "timezone", "UTC", "Timezone for the schedule")
	cmd.Flags().BoolVar(&runOnDeploy, "run-on-deploy", false, "Run the function immediately after deployment")
	return cmd
}

func newScheduleListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all scheduled functions",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newScheduleGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get schedule for the current function",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleGet(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newScheduleRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove schedule from the current function",
		Aliases: []string{"delete", "rm", "unset"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleRemove()
		},
	}
	return cmd
}

func newSchedulePresetsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "List available schedule presets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchedulePresets(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newScheduleTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger",
		Short: "Manually trigger a scheduled function",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleTrigger()
		},
	}
	return cmd
}

func runScheduleSet(cronExpr, preset, timezone string, runOnDeploy bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	if err := requireVaultPlan(FeatureSchedule); err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	// If preset is specified, use it
	if preset != "" {
		presets := getSchedulePresetsMap()
		if p, ok := presets[preset]; ok {
			cronExpr = p.Cron
		} else {
			return fmt.Errorf("unknown preset: %s. Use 'ff schedule presets' to see available presets", preset)
		}
	}

	// Validate cron expression
	if cronExpr == "" {
		return fmt.Errorf("either a cron expression or a preset is required")
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Get function ID first
	var functionData map[string]interface{}
	path := fmt.Sprintf("/v1/registry/%s/%s", creds.User.Username, manifest.Name)
	if err := client.Get(path, &functionData); err != nil {
		return fmt.Errorf("could not fetch function: %w", err)
	}

	functionID, ok := functionData["id"].(string)
	if !ok {
		return fmt.Errorf("could not find function ID")
	}

	// Create schedule
	schedulePath := fmt.Sprintf("/v1/functions/%s/schedule", functionID)
	reqBody := map[string]interface{}{
		"cron":          cronExpr,
		"timezone":      timezone,
		"run_on_deploy": runOnDeploy,
	}

	var response map[string]interface{}
	if err := client.Post(schedulePath, reqBody, &response); err != nil {
		return fmt.Errorf("could not set schedule: %w", err)
	}

	fmt.Printf("✅ Schedule created for %s\n", manifest.Name)
	fmt.Printf("   Cron: %s\n", cronExpr)
	if desc, ok := response["description"].(string); ok && desc != "" {
		fmt.Printf("   %s\n", desc)
	}
	fmt.Printf("   Timezone: %s\n", timezone)

	return nil
}

func runScheduleList(asJSON bool) error {
	creds, err := LoadCredentials()
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var schedules []ScheduleInfo
	path := "/v1/schedules"
	if err := client.Get(path, &schedules); err != nil {
		return fmt.Errorf("could not fetch schedules: %w", err)
	}

	if asJSON {
		printJSON(schedules)
		return nil
	}

	if len(schedules) == 0 {
		fmt.Println("No scheduled functions.")
		fmt.Println("   → Use: ff schedule set <cron-expression>")
		return nil
	}

	fmt.Printf("Scheduled functions for %s:\n\n", creds.User.Username)
	for _, s := range schedules {
		status := "🟢 Enabled"
		if !s.Enabled {
			status = "🔴 Disabled"
		}
		fmt.Printf("  📦 %s\n", status)
		fmt.Printf("     Cron: %s\n", s.Cron)
		if s.Description != "" {
			fmt.Printf("     %s\n", s.Description)
		}
		if !s.NextRun.IsZero() {
			fmt.Printf("     Next run: %s\n", s.NextRun.Format(time.RFC1123))
		}
		fmt.Println()
	}

	return nil
}

func runScheduleGet(asJSON bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Get function ID first
	var functionData map[string]interface{}
	path := fmt.Sprintf("/v1/registry/%s/%s", creds.User.Username, manifest.Name)
	if err := client.Get(path, &functionData); err != nil {
		return fmt.Errorf("could not fetch function: %w", err)
	}

	functionID, ok := functionData["id"].(string)
	if !ok {
		return fmt.Errorf("could not find function ID")
	}

	// Get schedule
	var schedule ScheduleInfo
	schedulePath := fmt.Sprintf("/v1/functions/%s/schedule", functionID)
	if err := client.Get(schedulePath, &schedule); err != nil {
		return fmt.Errorf("could not fetch schedule: %w", err)
	}

	if asJSON {
		printJSON(schedule)
		return nil
	}

	status := "🟢 Enabled"
	if !schedule.Enabled {
		status = "🔴 Disabled"
	}
	fmt.Printf("Schedule for %s:\n\n", manifest.Name)
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Cron: %s\n", schedule.Cron)
	if schedule.Description != "" {
		fmt.Printf("  %s\n", schedule.Description)
	}
	fmt.Printf("  Timezone: %s\n", schedule.Timezone)
	if !schedule.LastRun.IsZero() {
		fmt.Printf("  Last run: %s\n", schedule.LastRun.Format(time.RFC1123))
	}
	if !schedule.NextRun.IsZero() {
		fmt.Printf("  Next run: %s\n", schedule.NextRun.Format(time.RFC1123))
	}

	return nil
}

func runScheduleRemove() error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Get function ID first
	var functionData map[string]interface{}
	path := fmt.Sprintf("/v1/registry/%s/%s", creds.User.Username, manifest.Name)
	if err := client.Get(path, &functionData); err != nil {
		return fmt.Errorf("could not fetch function: %w", err)
	}

	functionID, ok := functionData["id"].(string)
	if !ok {
		return fmt.Errorf("could not find function ID")
	}

	// Remove schedule
	schedulePath := fmt.Sprintf("/v1/functions/%s/schedule", functionID)
	if err := client.Delete(schedulePath, nil); err != nil {
		return fmt.Errorf("could not remove schedule: %w", err)
	}

	fmt.Printf("✅ Schedule removed from %s\n", manifest.Name)
	return nil
}

func runSchedulePresets(asJSON bool) error {
	presets := []SchedulePreset{
		{Name: "every-minute", Cron: "* * * * *", Description: "Runs every minute"},
		{Name: "every-5-minutes", Cron: "*/5 * * * *", Description: "Runs every 5 minutes"},
		{Name: "every-15-minutes", Cron: "*/15 * * * *", Description: "Runs every 15 minutes"},
		{Name: "every-30-minutes", Cron: "*/30 * * * *", Description: "Runs every 30 minutes"},
		{Name: "every-hour", Cron: "0 * * * *", Description: "Runs at the start of every hour"},
		{Name: "every-day", Cron: "0 0 * * *", Description: "Runs once a day at midnight"},
		{Name: "every-noon", Cron: "0 12 * * *", Description: "Runs once a day at noon"},
		{Name: "weekdays", Cron: "0 0 * * 1-5", Description: "Runs Monday through Friday at midnight"},
		{Name: "every-week", Cron: "0 0 * * 0", Description: "Runs on Sunday at midnight"},
		{Name: "every-month", Cron: "0 0 1 * *", Description: "Runs on the first day of every month"},
	}

	if asJSON {
		printJSON(presets)
		return nil
	}

	fmt.Println("Available schedule presets:")
	fmt.Println()
	for _, p := range presets {
		fmt.Printf("  %s\n", p.Name)
		fmt.Printf("    Cron: %s\n", p.Cron)
		fmt.Printf("    %s\n", p.Description)
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  ff schedule set --preset every-hour")
	fmt.Println("  ff schedule set \"0 0 * * *\"")
	return nil
}

func runScheduleTrigger() error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	if err := requireVaultPlan(FeatureSchedule); err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Get function ID first
	var functionData map[string]interface{}
	path := fmt.Sprintf("/v1/registry/%s/%s", creds.User.Username, manifest.Name)
	if err := client.Get(path, &functionData); err != nil {
		return fmt.Errorf("could not fetch function: %w", err)
	}

	functionID, ok := functionData["id"].(string)
	if !ok {
		return fmt.Errorf("could not find function ID")
	}

	// Trigger schedule
	triggerPath := fmt.Sprintf("/v1/functions/%s/schedule/trigger", functionID)
	var response map[string]interface{}
	if err := client.Post(triggerPath, nil, &response); err != nil {
		return fmt.Errorf("could not trigger function: %w", err)
	}

	fmt.Printf("✅ Triggered %s\n", manifest.Name)
	return nil
}

func getSchedulePresetsMap() map[string]SchedulePreset {
	return map[string]SchedulePreset{
		"every-minute":     {Name: "every-minute", Cron: "* * * * *", Description: "Runs every minute"},
		"every-5-minutes":  {Name: "every-5-minutes", Cron: "*/5 * * * *", Description: "Runs every 5 minutes"},
		"every-15-minutes": {Name: "every-15-minutes", Cron: "*/15 * * * *", Description: "Runs every 15 minutes"},
		"every-30-minutes": {Name: "every-30-minutes", Cron: "*/30 * * * *", Description: "Runs every 30 minutes"},
		"every-hour":       {Name: "every-hour", Cron: "0 * * * *", Description: "Runs at the start of every hour"},
		"every-day":        {Name: "every-day", Cron: "0 0 * * *", Description: "Runs once a day at midnight"},
		"every-noon":       {Name: "every-noon", Cron: "0 12 * * *", Description: "Runs once a day at noon"},
		"weekdays":         {Name: "weekdays", Cron: "0 0 * * 1-5", Description: "Runs Monday through Friday at midnight"},
		"every-week":       {Name: "every-week", Cron: "0 0 * * 0", Description: "Runs on Sunday at midnight"},
		"every-month":      {Name: "every-month", Cron: "0 0 1 * *", Description: "Runs on the first day of every month"},
	}
}
