package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewRollbackCmd() *cobra.Command {
	var version string
	var force bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "rollback [author/name]",
		Short:   "Roll back to a previous version",
		Example: "  ff rollback\n  ff rollback alice/my-fn\n  ff rollback --version 1.0.5\n  ff rollback --force",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(args, version, force, asJSON)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Version to roll back to (default: previous)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

type VersionInfo struct {
	Version    string `json:"version"`
	DeployedAt string `json:"deployed_at"`
	Hash       string `json:"hash"`
	Active     bool   `json:"active"`
}

func runRollback(args []string, targetVersion string, force, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}
	var versions []VersionInfo
	path := fmt.Sprintf("/v1/registry/functions/%s/%s/versions", author, name)
	if err := client.Get(path, &versions); err != nil {
		return fmt.Errorf("could not fetch versions: %w", err)
	}
	if len(versions) < 2 {
		return fmt.Errorf("no previous versions available to roll back to")
	}
	var target *VersionInfo
	if targetVersion != "" {
		for i := range versions {
			if versions[i].Version == targetVersion {
				target = &versions[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("version %q not found\n   → Available versions: %s", targetVersion, formatVersionList(versions))
		}
	} else {
		for i := range versions {
			if !versions[i].Active {
				target = &versions[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("no previous version found to roll back to")
		}
	}
	var current *VersionInfo
	for i := range versions {
		if versions[i].Active {
			current = &versions[i]
			break
		}
	}
	if !asJSON {
		if current != nil {
			fmt.Printf("Current version: %s\n", current.Version)
		}
		fmt.Printf("Roll back to:    %s\n\n", target.Version)
	}
	if !force && IsInteractive() && !asJSON && !WantJSON() {
		confirmed := PromptConfirm(fmt.Sprintf("Roll back %s/%s to v%s?", author, name, target.Version), false)
		if !confirmed {
			fmt.Println("Rollback cancelled.")
			return nil
		}
	}
	rollbackReq := map[string]interface{}{"version": target.Version}
	var result map[string]interface{}
	rollbackPath := fmt.Sprintf("/v1/registry/functions/%s/%s/rollback", author, name)
	if err := client.Post(rollbackPath, rollbackReq, &result); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}
	if asJSON || WantJSON() {
		prevVersion := ""
		if current != nil {
			prevVersion = current.Version
		}
		printJSON(map[string]interface{}{"success": true, "function": name, "author": author, "rolled_back_to": target.Version, "previous_version": prevVersion})
		return nil
	}
	fmt.Printf("✅ Rolled back %s/%s to v%s\n", author, name, target.Version)
	fmt.Printf("\nRun 'ff test' to verify the rollback.\n")
	return nil
}

func formatVersionList(versions []VersionInfo) string {
	result := ""
	for i, v := range versions {
		if i > 0 {
			result += ", "
		}
		if v.Active {
			result += v.Version + " (active)"
		} else {
			result += v.Version
		}
	}
	return result
}
