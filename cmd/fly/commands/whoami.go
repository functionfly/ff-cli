package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func NewWhoamiCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "whoami",
		Short:   "Show the currently logged-in user",
		Example: "  ff whoami\n  ff whoami --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runWhoami(asJSON bool) error {
	creds, err := LoadCredentials()
	if err != nil {
		return err
	}
	if asJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":         creds.User.ID,
			"username":   creds.User.Username,
			"email":      creds.User.Email,
			"provider":   creds.User.Provider,
			"expires_at": creds.ExpiresAt,
			"namespace":  fmt.Sprintf("fx://%s/*", creds.User.Username),
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("👤 %s\n", creds.User.Username)
	if creds.User.Email != "" {
		fmt.Printf("   Email:     %s\n", creds.User.Email)
	}
	fmt.Printf("   Provider:  %s\n", creds.User.Provider)
	fmt.Printf("   Namespace: fx://%s/*\n", creds.User.Username)
	if !creds.ExpiresAt.IsZero() {
		fmt.Printf("   Expires:   %s\n", creds.ExpiresAt.Format("2006-01-02 15:04 UTC"))
		fmt.Printf("   Session:   %s remaining\n", SessionExpiresIn())
	}
	return nil
}
