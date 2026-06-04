package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewLogoutCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "logout",
		Short:   "Clear stored credentials and log out",
		Example: "  ff logout\n  ff logout --force",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func runLogout(force bool) error {
	creds, err := LoadCredentials()
	if err != nil {
		// If there's no credentials file (or any other "no creds" case),
		// try to clear anything that might still be on disk / in the
		// keychain and report a clean status.
		if delErr := DeleteCredentials(); delErr != nil {
			return fmt.Errorf("not logged in — nothing to log out from\n   → Run: ff login")
		}
		fmt.Println("You're already logged out.")
		return nil
	}
	// Skip prompt in non-interactive (CI) or when --force is set.
	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(fmt.Sprintf("Log out %s?", creds.User.Username), false)
		if !confirmed {
			fmt.Println("Logout cancelled.")
			return nil
		}
	}
	if err := DeleteCredentials(); err != nil {
		return fmt.Errorf("could not remove credentials: %w", err)
	}
	fmt.Printf("✅ Logged out %s\n", creds.User.Username)
	fmt.Println("   Run 'ff login' to authenticate again.")
	return nil
}
