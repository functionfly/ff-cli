package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:     "user",
	Aliases: []string{"profile"},
	Short:   "Manage user profile",
	Long: `View and update your user profile settings.

Update your username, email, and other account settings. Some changes
may require re-authentication.`,
	Example: `  ff user show
  ff user update --username new-name
  ff user update --email new@example.com
  ff user settings`,
	SilenceUsage: true,
}

func init() {
	userCmd.AddCommand(newUserShowCmd())
	userCmd.AddCommand(newUserUpdateCmd())
	userCmd.AddCommand(newUserSettingsCmd())
}

func UserCmd() *cobra.Command {
	return userCmd
}

type UserProfile struct {
	ID           string            `json:"id"`
	Username     string            `json:"username"`
	Email        string            `json:"email"`
	Provider     string            `json:"provider"`
	AvatarURL    string            `json:"avatar_url,omitempty"`
	Plan         string            `json:"plan,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	UpdatedAt    string            `json:"updated_at,omitempty"`
	Settings     map[string]bool   `json:"settings,omitempty"`
}

func newUserShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "show",
		Aliases: []string{"info", "get"},
		Short:   "Show current user profile",
		Long:    `Display the current user's profile information from the API.`,
		Example: `  ff user show
  ff user show --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserShow(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runUserShow(asJSON bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var profile UserProfile
	if err := client.Get("/v1/users/me", &profile); err != nil {
		return fmt.Errorf("could not fetch profile: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(profile)
		return nil
	}

	fmt.Printf("\n👤 %s\n", profile.Username)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  ID:       %s\n", profile.ID)
	fmt.Printf("  Email:    %s\n", profile.Email)
	fmt.Printf("  Provider: %s\n", profile.Provider)
	if profile.Plan != "" {
		fmt.Printf("  Plan:     %s\n", planDisplayName(profile.Plan))
	} else {
		fmt.Printf("  Plan:     %s\n", planDisplayName(creds.User.Plan))
	}
	if profile.AvatarURL != "" {
		fmt.Printf("  Avatar:   %s\n", profile.AvatarURL)
	}
	fmt.Printf("  Namespace: fx://%s/*\n", profile.Username)
	if profile.CreatedAt != "" {
		fmt.Printf("  Created:  %s\n", profile.CreatedAt)
	}

	if len(profile.Settings) > 0 {
		fmt.Printf("\n  Settings:\n")
		for k, v := range profile.Settings {
			status := "off"
			if v {
				status = "on"
			}
			fmt.Printf("    %-28s %s\n", k, status)
		}
	}

	fmt.Println()
	return nil
}

func newUserUpdateCmd() *cobra.Command {
	var username string
	var email string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update user profile",
		Long: `Update your username or email. Changing the username will update
your public namespace (fx://username/*). Some changes may require
re-authentication.`,
		Example: `  ff user update --username new-name
  ff user update --email new@example.com
  ff user update --username new-name --email new@example.com --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserUpdate(username, email, asJSON)
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "New username")
	cmd.Flags().StringVar(&email, "email", "", "New email address")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runUserUpdate(username, email string, asJSON bool) error {
	if username == "" && email == "" {
		return fmt.Errorf("specify at least one of --username or --email")
	}

	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	body := map[string]interface{}{}
	if username != "" {
		body["username"] = username
	}
	if email != "" {
		body["email"] = email
	}

	var profile UserProfile
	if err := client.Patch("/v1/users/me", body, &profile); err != nil {
		return fmt.Errorf("could not update profile: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(profile)
		return nil
	}

	fmt.Printf("✅ Profile updated\n")
	if username != "" {
		fmt.Printf("   Username:  %s\n", profile.Username)
		fmt.Printf("   Namespace: fx://%s/*\n", profile.Username)
	}
	if email != "" {
		fmt.Printf("   Email:     %s\n", profile.Email)
	}
	return nil
}

func newUserSettingsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "View and manage account settings",
		Long:  `Display current account settings and preferences.`,
		Example: `  ff user settings
  ff user settings --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserSettings(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

type UserSettings struct {
	EmailNotifications bool `json:"email_notifications"`
	MarketingEmails    bool `json:"marketing_emails"`
	PublicProfile      bool `json:"public_profile"`
	TwoFactor          bool `json:"two_factor"`
	WebhooksEnabled    bool `json:"webhooks_enabled"`
}

func runUserSettings(asJSON bool) error {
	_, err := requireAuth()
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var settings UserSettings
	if err := client.Get("/v1/users/me/settings", &settings); err != nil {
		return fmt.Errorf("could not fetch settings: %w", err)
	}

	if asJSON || WantJSON() {
		printJSON(settings)
		return nil
	}

	fmt.Printf("\n⚙️  Account Settings\n")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  Email notifications:  %s\n", boolLabel(settings.EmailNotifications))
	fmt.Printf("  Marketing emails:     %s\n", boolLabel(settings.MarketingEmails))
	fmt.Printf("  Public profile:       %s\n", boolLabel(settings.PublicProfile))
	fmt.Printf("  Two-factor auth:      %s\n", boolLabel(settings.TwoFactor))
	fmt.Printf("  Webhooks:             %s\n", boolLabel(settings.WebhooksEnabled))
	fmt.Printf("\n  Manage settings at: https://functionfly.com/settings\n")
	fmt.Println()
	return nil
}

func boolLabel(v bool) string {
	if v {
		return "✅ on"
	}
	return "❌ off"
}
