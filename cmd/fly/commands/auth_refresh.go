package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func NewAuthRefreshCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the current authentication session",
		Long:  "Refresh the stored token using the stored refresh token. If the session has expired or is about to expire, use this command to extend it without logging in again.\n\nRequires a stored refresh token. Run 'ff login' first if you have no credentials.",
		Example: `  ff auth refresh
  ff auth refresh --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthRefresh(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Refresh even if token is not expiring soon")
	return cmd
}

func runAuthRefresh(force bool) error {
	ctx := context.Background()

	creds, err := LoadCredentials()
	if err != nil {
		return fmt.Errorf("not logged in\n   → Run: ff login")
	}

	// Check if token is expiring within 24 hours (or forced)
	if !force {
		if creds.ExpiresAt.IsZero() || time.Until(creds.ExpiresAt) > 24*time.Hour {
			remaining := "unknown"
			if !creds.ExpiresAt.IsZero() {
				daysLeft := int(time.Until(creds.ExpiresAt).Hours() / 24)
				remaining = fmt.Sprintf("%.0f days", float64(daysLeft))
			}
			fmt.Printf("Session is not expiring soon (expires in %s).\nUse --force to refresh anyway.\n", remaining)
			return nil
		}
	}

	if creds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available — your session cannot be refreshed\n   → Run: ff logout && ff login")
	}

	fmt.Printf("Refreshing session...\n")
	newCreds, err := RefreshCredentials(ctx)
	if err != nil {
		return fmt.Errorf("session refresh failed: %w\n   → Run: ff logout && ff login", err)
	}

	daysLeft := int(time.Until(newCreds.ExpiresAt).Hours() / 24)
	fmt.Printf("✅ Session refreshed — now expires in %d days\n", daysLeft)
	return nil
}
