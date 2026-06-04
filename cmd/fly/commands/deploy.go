package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewDeployCmd creates the `ff deploy` command.
// It publishes the function and then optionally:
//   - Tags the resulting version with an environment alias (--env staging|production)
//   - Starts a canary deployment at the given traffic percentage (--canary N)
func NewDeployCmd() *cobra.Command {
	var env string
	var canaryPercent int
	var access string
	var force bool
	var dryRun bool
	var asJSON bool
	var skipTypeCheck bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Publish and promote a function to an environment",
		Long: `Publish your function and promote it to a named environment (staging or
production) or start a canary rollout. Under the hood 'ff deploy' runs
'ff publish' and then sets the appropriate version alias.

  ff deploy --env production          Publish and set as production
  ff deploy --env staging             Publish and set as staging
  ff deploy --canary 10               Publish and start canary at 10%
  ff deploy --env production --force  Skip confirmation`,
		Example: "  ff deploy --env production\n  ff deploy --env staging\n  ff deploy --canary 10\n  ff deploy --env production --access public",
		RunE: func(cmd *cobra.Command, args []string) error {
			if env == "" && canaryPercent == 0 {
				return fmt.Errorf("specify --env (staging|production) or --canary <percent>")
			}
			if env != "" && env != "staging" && env != "production" {
				return fmt.Errorf("--env must be 'staging' or 'production'")
			}
			if canaryPercent != 0 && (canaryPercent < 1 || canaryPercent > 99) {
				return fmt.Errorf("--canary must be between 1 and 99")
			}
			return runDeploy(env, canaryPercent, access, force, dryRun, asJSON, skipTypeCheck)
		},
	}

	cmd.Flags().StringVar(&env, "env", "", "Target environment: staging or production")
	cmd.Flags().IntVar(&canaryPercent, "canary", 0, "Publish and start a canary at this traffic percentage (1–99)")
	cmd.Flags().StringVar(&access, "access", "", "Access level: public or private")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and bundle without publishing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&skipTypeCheck, "skip-type-check", false, "Skip TypeScript type checking")
	return cmd
}

func runDeploy(env string, canaryPercent int, access string, force, dryRun, asJSON, skipTypeCheck bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	label := env
	if canaryPercent > 0 {
		label = fmt.Sprintf("canary@%d%%", canaryPercent)
	}

	if !force && !YesMode && IsInteractive() && !asJSON && !WantJSON() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Deploy %s v%s → %s?", manifest.Name, manifest.Version, label),
			true,
		)
		if !confirmed {
			fmt.Println("Deploy cancelled.")
			return nil
		}
	}

	// Step 1: publish
	if !asJSON && !WantJSON() {
		fmt.Printf("🚀 Publishing %s v%s...\n", manifest.Name, manifest.Version)
	}
	if err := runPublish(access, force, false, dryRun, asJSON, skipTypeCheck); err != nil {
		return err
	}
	if dryRun {
		return nil
	}

	author := creds.User.Username
	name := manifest.Name
	version := manifest.Version

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Step 2a: set environment alias
	if env != "" {
		if !asJSON && !WantJSON() {
			fmt.Printf("\n🏷️  Tagging v%s as %q...\n", version, env)
		}
		// GET function ID
		var fn struct {
			ID string `json:"id"`
		}
		if err := client.Get(fmt.Sprintf("/v1/registry/functions/%s/%s", author, name), &fn); err != nil {
			return fmt.Errorf("could not look up function: %w", err)
		}
		aliasPath := fmt.Sprintf("/v1/functions/%s/versions/%s/alias/%s", fn.ID, version, env)
		var aliasResult map[string]interface{}
		if err := client.Post(aliasPath, map[string]interface{}{}, &aliasResult); err != nil {
			return fmt.Errorf("could not set %q alias: %w", env, err)
		}
		if asJSON || WantJSON() {
			printJSON(map[string]interface{}{
				"function": name, "author": author,
				"version": version, "env": env,
			})
			return nil
		}
		fmt.Printf("✅ %s/%s v%s deployed to %s\n", author, name, version, env)
		return nil
	}

	// Step 2b: start canary
	if canaryPercent > 0 {
		if !asJSON && !WantJSON() {
			fmt.Printf("\n🐤 Starting canary at %d%%...\n", canaryPercent)
		}
		req := map[string]interface{}{
			"version":         version,
			"traffic_percent": canaryPercent,
		}
		var canary CanaryConfig
		canaryPath := fmt.Sprintf("/v1/registry/functions/%s/%s/canary", author, name)
		if err := client.Post(canaryPath, req, &canary); err != nil {
			return fmt.Errorf("published but could not start canary: %w\n   → Run: ff canary start --version %s --percent %d", err, version, canaryPercent)
		}
		if asJSON || WantJSON() {
			printJSON(canary)
			return nil
		}
		fmt.Printf("✅ %s/%s v%s deployed as canary (%d%% traffic)\n\n", author, name, version, canaryPercent)
		fmt.Printf("Next steps:\n")
		fmt.Printf("  ff canary status               — check metrics\n")
		fmt.Printf("  ff canary promote --percent 50  — increase traffic\n")
		fmt.Printf("  ff canary promote --full         — complete rollout\n")
		fmt.Printf("  ff canary rollback               — revert if issues\n")
	}
	return nil
}
