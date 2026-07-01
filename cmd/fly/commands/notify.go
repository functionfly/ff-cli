package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:     "notify",
	Aliases: []string{"webhook", "alerts", "notifications"},
	Short:   "Manage webhooks and notifications",
	Long: `Manage webhook endpoints and notification rules for your functions.

Set up webhooks to receive real-time alerts when functions fail, exceed
latency thresholds, hit rate limits, or trigger custom events.

Supports Slack, Discord, PagerDuty, email, and generic HTTP webhooks.`,
	Example: `  ff notify list
  ff notify create --url https://hooks.slack.com/... --events error,deploy
  ff notify create --url https://example.com/hook --events all --secret my-signing-key
  ff notify delete <id>
  ff notify test <id>`,
	SilenceUsage: true,
}

func init() {
	notifyCmd.AddCommand(newNotifyListCmd())
	notifyCmd.AddCommand(newNotifyCreateCmd())
	notifyCmd.AddCommand(newNotifyDeleteCmd())
	notifyCmd.AddCommand(newNotifyUpdateCmd())
	notifyCmd.AddCommand(newNotifyTestCmd())
}

func NotifyCmd() *cobra.Command {
	return notifyCmd
}

type Webhook struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Secret    string   `json:"secret,omitempty"`
	Active    bool     `json:"active"`
	Format    string   `json:"format,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	LastFired string   `json:"last_fired,omitempty"`
	FailCount int      `json:"fail_count,omitempty"`
}

type WebhookListResponse struct {
	Webhooks []Webhook `json:"webhooks"`
	Total    int       `json:"total,omitempty"`
}

var validEvents = []string{
	"error", "deploy", "rollback", "scale", "rate_limit",
	"health_change", "canary", "domain_change", "all",
}

// --- list ---

func newNotifyListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List notification webhooks",
		Long:    `List all configured webhook endpoints and their status.`,
		Example: `  ff notify list
  ff notify list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyList(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runNotifyList(asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var resp WebhookListResponse
	if err := client.Get("/v1/notifications/webhooks", &resp); err != nil {
		return fmt.Errorf("could not list webhooks: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Webhooks) == 0 {
		fmt.Println("No webhooks configured.")
		fmt.Println("\nCreate one with: ff notify create --url <url> --events error,deploy")
		return nil
	}

	fmt.Printf("\n🔔 Webhooks (%d)\n\n", len(resp.Webhooks))
	fmt.Printf("  %-12s  %-44s  %-20s  %-8s  %s\n", "ID", "URL", "EVENTS", "STATUS", "LAST FIRED")
	fmt.Println("  " + strings.Repeat("-", 100))

	for _, w := range resp.Webhooks {
		id := w.ID
		if len(id) > 10 {
			id = id[:10]
		}
		url := w.URL
		if len(url) > 42 {
			url = url[:39] + "..."
		}
		events := strings.Join(w.Events, ",")
		if len(events) > 18 {
			events = events[:15] + "..."
		}
		status := "✅ active"
		if !w.Active {
			status = "❌ inactive"
		}
		if w.FailCount > 3 {
			status = fmt.Sprintf("⚠️  failing (%d)", w.FailCount)
		}
		lastFired := w.LastFired
		if len(lastFired) > 10 {
			lastFired = lastFired[:10]
		}
		if lastFired == "" {
			lastFired = "never"
		}
		fmt.Printf("  %-12s  %-44s  %-20s  %-8s  %s\n", id, url, events, status, lastFired)
	}

	fmt.Println()
	return nil
}

// --- create ---

func newNotifyCreateCmd() *cobra.Command {
	var url string
	var events []string
	var secret string
	var format string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a webhook endpoint",
		Long: `Create a new webhook endpoint to receive event notifications.

Events are delivered as HTTP POST requests with a JSON payload.
Use --secret to enable HMAC signature verification.`,
		Example: `  ff notify create --url https://hooks.slack.com/services/... --events error,deploy
  ff notify create --url https://example.com/hook --events all
  ff notify create --url https://discord.com/api/webhooks/... --events error --format discord
  ff notify create --url https://events.pagerduty.com/... --events error,health_change`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyCreate(url, events, secret, format, asJSON)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "Webhook URL (required)")
	cmd.Flags().StringSliceVar(&events, "events", nil, "Events to subscribe to (comma-separated: error,deploy,rollback,all)")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC signing secret for payload verification")
	cmd.Flags().StringVar(&format, "format", "json", "Payload format: json, slack, discord, pagerduty")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("events")
	return cmd
}

func runNotifyCreate(url string, events []string, secret, format string, asJSON bool) error {
	if err := validateEvents(events); err != nil {
		return err
	}

	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"url":    url,
		"events": events,
		"format": format,
	}
	if secret != "" {
		body["secret"] = secret
	}

	var webhook Webhook
	if err := client.Post("/v1/notifications/webhooks", body, &webhook); err != nil {
		return fmt.Errorf("could not create webhook: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(webhook)
		return nil
	}

	fmt.Printf("✅ Created webhook %s\n", webhook.ID)
	fmt.Printf("   URL:     %s\n", webhook.URL)
	fmt.Printf("   Events:  %s\n", strings.Join(webhook.Events, ", "))
	fmt.Printf("   Format:  %s\n", format)
	if secret != "" {
		fmt.Printf("   Secret:  (configured)\n")
	}
	return nil
}

// --- delete ---

func newNotifyDeleteCmd() *cobra.Command {
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a webhook",
		Long:    `Delete a webhook endpoint. The endpoint will stop receiving events immediately.`,
		Example: `  ff notify delete <id>
  ff notify delete <id> --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyDelete(args[0], force, asJSON)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runNotifyDelete(id string, force, asJSON bool) error {
	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(fmt.Sprintf("Delete webhook %s?", id), false)
		if !confirmed {
			fmt.Println("Aborted.")
			return nil
		}
	}

	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	if err := client.Delete("/v1/notifications/webhooks/"+id, nil); err != nil {
		return fmt.Errorf("could not delete webhook: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{"success": true, "id": id, "deleted": true})
		return nil
	}

	fmt.Printf("✅ Deleted webhook %s\n", id)
	return nil
}

// --- update ---

func newNotifyUpdateCmd() *cobra.Command {
	var url string
	var events []string
	var active *bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a webhook",
		Long:  `Update a webhook's URL, events, or active status.`,
		Example: `  ff notify update <id> --events error,deploy,rollback
  ff notify update <id> --url https://new-url.com/hook
  ff notify update <id> --disable
  ff notify update <id> --enable`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyUpdate(args[0], url, events, active, asJSON)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "New webhook URL")
	cmd.Flags().StringSliceVar(&events, "events", nil, "New events list")
	cmd.Flags().Bool("enable", false, "Enable the webhook")
	cmd.Flags().Bool("disable", false, "Disable the webhook")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runNotifyUpdate(id, url string, events []string, active *bool, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{}
	if url != "" {
		body["url"] = url
	}
	if len(events) > 0 {
		if err := validateEvents(events); err != nil {
			return err
		}
		body["events"] = events
	}
	if active != nil {
		body["active"] = *active
	}

	var webhook Webhook
	if err := client.Patch("/v1/notifications/webhooks/"+id, body, &webhook); err != nil {
		return fmt.Errorf("could not update webhook: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(webhook)
		return nil
	}

	fmt.Printf("✅ Updated webhook %s\n", id)
	if url != "" {
		fmt.Printf("   URL:     %s\n", webhook.URL)
	}
	if len(events) > 0 {
		fmt.Printf("   Events:  %s\n", strings.Join(webhook.Events, ", "))
	}
	return nil
}

// --- test ---

func newNotifyTestCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "test <id>",
		Short: "Send a test event to a webhook",
		Long: `Send a test ping event to a webhook endpoint to verify it's
receiving events correctly.`,
		Example: `  ff notify test <id>
  ff notify test <id> --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifyTest(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runNotifyTest(id string, asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var result struct {
		OK       bool   `json:"ok"`
		Status   int    `json:"status,omitempty"`
		Message  string `json:"message,omitempty"`
		Duration int64  `json:"duration_ms,omitempty"`
	}
	if err := client.Post("/v1/notifications/webhooks/"+id+"/test", nil, &result); err != nil {
		return fmt.Errorf("could not test webhook: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}

	if result.OK {
		fmt.Printf("✅ Webhook %s received test event (HTTP %d, %dms)\n", id, result.Status, result.Duration)
	} else {
		fmt.Printf("❌ Webhook %s test failed: %s\n", id, result.Message)
	}
	return nil
}

func validateEvents(events []string) error {
	valid := make(map[string]bool, len(validEvents))
	for _, e := range validEvents {
		valid[e] = true
	}
	for _, e := range events {
		if !valid[e] {
			return fmt.Errorf("invalid event %q — valid events: %s", e, strings.Join(validEvents, ", "))
		}
	}
	return nil
}
