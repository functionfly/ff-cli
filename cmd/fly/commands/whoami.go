package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func NewWhoamiCmd() *cobra.Command {
	var asJSON bool
	var verify bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently logged-in user",
		Long: `Show the currently logged-in user. The default output reflects the
locally stored credentials file. With --verify, an authenticated round-trip
to the API is made to confirm the token is still valid.`,
		Example: "  ff whoami\n  ff whoami --json\n  ff whoami --verify",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami(asJSON, verify)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify credentials with a live API call")
	return cmd
}

func runWhoami(asJSON, verify bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	verified := false
	var verifyErr string
	if verify {
		client := NewAPIClientWithToken(creds.Token)
		if err := client.Get("/v1/users/me", &struct{}{}); err != nil {
			verifyErr = err.Error()
		} else {
			verified = true
		}
	}
	if asJSON {
		out := map[string]interface{}{
			"id":         creds.User.ID,
			"username":   creds.User.Username,
			"email":      creds.User.Email,
			"provider":   creds.User.Provider,
			"plan":       creds.User.Plan,
			"expires_at": creds.ExpiresAt,
			"namespace":  fmt.Sprintf("fx://%s/*", creds.User.Username),
		}
		if verify {
			out["verified"] = verified
			if verifyErr != "" {
				out["verify_error"] = verifyErr
			}
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		if verify && !verified {
			return fmt.Errorf("credential verification failed: %s", verifyErr)
		}
		return nil
	}
	fmt.Printf("👤 %s\n", creds.User.Username)
	if creds.User.Email != "" {
		fmt.Printf("   Email:     %s\n", creds.User.Email)
	}
	fmt.Printf("   Provider:  %s\n", creds.User.Provider)
	if creds.User.Plan != "" {
		fmt.Printf("   Plan:      %s\n", creds.User.Plan)
	}
	fmt.Printf("   Namespace: fx://%s/*\n", creds.User.Username)
	if !creds.ExpiresAt.IsZero() {
		fmt.Printf("   Expires:   %s\n", creds.ExpiresAt.Format("2006-01-02 15:04 UTC"))
		fmt.Printf("   Session:   %s remaining\n", SessionExpiresIn())
	}
	if verify {
		if verified {
			fmt.Println("   Verified:  ✅ live API round-trip succeeded")
		} else {
			fmt.Printf("   Verified:  ❌ live API round-trip failed: %s\n", verifyErr)
			return fmt.Errorf("credential verification failed: %s", verifyErr)
		}
	}
	return nil
}
