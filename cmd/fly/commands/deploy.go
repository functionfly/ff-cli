package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// NewDeployCmd creates the `ff deploy` command.
// It publishes the function and then optionally:
//   - Tags the resulting version with an environment alias (--env <name>)
//   - Starts a canary deployment at the given traffic percentage (--canary N)
//   - Promotes an existing version to another environment (--promote)
//   - Injects environment-specific variables (--env-file)
func NewDeployCmd() *cobra.Command {
	var env string
	var canaryPercent int
	var access string
	var force bool
	var dryRun bool
	var asJSON bool
	var skipTypeCheck bool
	var envFile string
	var promoteFrom string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Publish and promote a function to an environment",
		Long: `Publish your function and promote it to a named environment or start a
canary rollout. Under the hood 'ff deploy' runs 'ff publish' and then sets
the appropriate version alias.

Environments are first-class: use any name (staging, production, dev, qa,
preview, etc.) and deploy to multiple environments independently.

  ff deploy --env production          Publish and set as production
  ff deploy --env staging             Publish and set as staging
  ff deploy --env preview-pr-123      Deploy to a PR preview environment
  ff deploy --env-file .env.staging   Deploy with staging-specific env vars
  ff deploy --promote staging→prod    Promote staging version to production
  ff deploy --canary 10               Publish and start canary at 10%
  ff deploy status                    Show per-environment deployment state`,
		Example: `  ff deploy --env production
  ff deploy --env staging
  ff deploy --env dev --env-file .env.dev
  ff deploy --env qa --access private
  ff deploy --promote staging→production
  ff deploy --canary 10
  ff deploy --env production --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if promoteFrom != "" {
				return runDeployPromote(promoteFrom, env, force, asJSON)
			}
			if env == "" && canaryPercent == 0 {
				return fmt.Errorf("specify --env, --canary, or --promote")
			}
			if env != "" {
				if err := validateEnvName(env, force); err != nil {
					return err
				}
			}
			if canaryPercent != 0 && (canaryPercent < 1 || canaryPercent > 99) {
				return fmt.Errorf("--canary must be between 1 and 99")
			}
			return runDeploy(env, canaryPercent, access, force, dryRun, asJSON, skipTypeCheck, envFile)
		},
	}

	cmd.Flags().StringVar(&env, "env", "", "Target environment (any name: staging, production, dev, qa, preview, etc.)")
	cmd.Flags().IntVar(&canaryPercent, "canary", 0, "Publish and start a canary at this traffic percentage (1–99)")
	cmd.Flags().StringVar(&access, "access", "", "Access level: public or private")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts and bypass reserved environment name restrictions")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and bundle without publishing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&skipTypeCheck, "skip-type-check", false, "Skip TypeScript type checking")
	cmd.Flags().StringVar(&envFile, "env-file", "", "Inject env vars from file for this deployment (e.g. .env.staging)")
	cmd.Flags().StringVar(&promoteFrom, "promote", "", "Promote a version from one env to another (e.g. staging→production)")

	// Add subcommands
	cmd.AddCommand(newDeployStatusCmd())
	cmd.AddCommand(newDeployEnvsCmd())

	return cmd
}

// envNameRegex allows alphanumeric, hyphens, underscores, and dots.
var envNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// reservedEnvs are environment names that are reserved for platform features.
// These names have special meaning in the FunctionFly deployment system:
//   - production: Primary production environment
//   - staging: Pre-production testing environment
//   - dev: Development environment
//   - preview: Pull request preview environments
//   - canary: Traffic splitting/canary deployments
// Use --force to bypass this restriction if needed.
var reservedEnvs = map[string]bool{
	"production": true,
	"staging":    true,
	"dev":        true,
	"preview":    true,
	"canary":     true,
}

func validateEnvName(name string, skipReserved bool) error {
	if name == "" {
		return fmt.Errorf("environment name cannot be empty")
	}
	if !envNameRegex.MatchString(name) {
		return fmt.Errorf("invalid environment name %q — use alphanumeric, hyphens, underscores, dots (max 64 chars, must start with alphanumeric)", name)
	}
	if !skipReserved && reservedEnvs[name] {
		return fmt.Errorf("environment name %q is reserved (use --force to override)", name)
	}
	return nil
}

func runDeploy(env string, canaryPercent int, access string, force, dryRun, asJSON, skipTypeCheck bool, envFile string) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}
	if canaryPercent > 0 {
		if err := requireVaultPlan(FeatureCanary); err != nil {
			return err
		}
	}
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	label := env
	if canaryPercent > 0 {
		label = fmt.Sprintf("canary@%d%%", canaryPercent)
	}

	// Inject env-file variables if provided
	if envFile != "" {
		if err := injectEnvFile(envFile, env, asJSON); err != nil {
			return err
		}
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
				"url": fmt.Sprintf("https://%s/%s/%s", "api.functionfly.com", author, name),
			})
			return nil
		}
		fmt.Printf("✅ %s/%s v%s → %s\n", author, name, version, env)
		fmt.Printf("\n  Endpoint:  https://%s/%s/%s\n", "api.functionfly.com", author, name)
		fmt.Printf("  Env:       %s\n", env)
		fmt.Printf("  Version:   %s\n", version)
		if envFile != "" {
			fmt.Printf("  Env-file:  %s (applied)\n", envFile)
		}
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

// injectEnvFile reads a .env-style file and applies its variables to the
// deployed function's environment, scoped to the target environment name.
func injectEnvFile(envFile, envName string, asJSON bool) error {
	absPath, err := filepath.Abs(envFile)
	if err != nil {
		return fmt.Errorf("could not resolve env file %q: %w", envFile, err)
	}
	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("could not open env file %s: %w", absPath, err)
	}
	defer f.Close()

	pairs := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
			v = v[1 : len(v)-1]
		}
		pairs[key] = v
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading %s: %w", envFile, err)
	}

	if len(pairs) == 0 {
		if !asJSON {
			fmt.Printf("  ⚠️  No variables found in %s\n", envFile)
		}
		return nil
	}

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

	// Merge with existing env vars
	envPath := fmt.Sprintf("/v1/registry/%s/%s/env", creds.User.Username, manifest.Name)
	var existing map[string]string
	if err := client.Get(envPath, &existing); err != nil {
		existing = map[string]string{}
	}
	for k, v := range pairs {
		existing[k] = v
	}
	if err := client.Put(envPath, existing, nil); err != nil {
		return fmt.Errorf("could not apply env-file variables: %w", err)
	}

	if !asJSON {
		fmt.Printf("  📄 Applied %d variable(s) from %s\n", len(pairs), filepath.Base(envFile))
	}
	return nil
}

// runDeployPromote promotes a version from one environment to another
// without re-publishing. Format: "source→target" or "source->target".
func runDeployPromote(promote, targetEnv string, force, asJSON bool) error {
	source, target, err := parsePromote(promote, targetEnv, force)
	if err != nil {
		return err
	}

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

	author := creds.User.Username
	name := manifest.Name

	// Resolve the source environment's version
	var fn struct {
		ID string `json:"id"`
	}
	if err := client.Get(fmt.Sprintf("/v1/registry/functions/%s/%s", author, name), &fn); err != nil {
		return fmt.Errorf("could not look up function: %w", err)
	}

	var versions []VersionInfo
	verPath := fmt.Sprintf("/v1/registry/functions/%s/%s/versions", author, name)
	if err := client.Get(verPath, &versions); err != nil {
		return fmt.Errorf("could not fetch versions: %w", err)
	}

	// Find the active version for the source environment
	sourceVersion := ""
	for _, v := range versions {
		if v.Active {
			sourceVersion = v.Version
			break
		}
	}
	if sourceVersion == "" {
		// Fall back to latest
		for _, v := range versions {
			sourceVersion = v.Version
			break
		}
	}
	if sourceVersion == "" {
		return fmt.Errorf("no version found for %s/%s", author, name)
	}

	if !force && !YesMode && IsInteractive() && !asJSON && !WantJSON() {
		confirmed := PromptConfirm(
			fmt.Sprintf("Promote %s/%s v%s from %s → %s?", author, name, sourceVersion, source, target),
			true,
		)
		if !confirmed {
			fmt.Println("Promote cancelled.")
			return nil
		}
	}

	aliasPath := fmt.Sprintf("/v1/functions/%s/versions/%s/alias/%s", fn.ID, sourceVersion, target)
	var aliasResult map[string]interface{}
	if err := client.Post(aliasPath, map[string]interface{}{}, &aliasResult); err != nil {
		return fmt.Errorf("could not promote %s → %s: %w", source, target, err)
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"function": name, "author": author,
			"version": sourceVersion,
			"from": source, "to": target,
		})
		return nil
	}

	fmt.Printf("✅ Promoted %s/%s v%s: %s → %s\n", author, name, sourceVersion, source, target)
	return nil
}

// parsePromote parses the promote flag value. Accepts:
//
//	"staging→production"  (Unicode arrow)
//	"staging->production" (ASCII arrow)
//	"staging"             (uses --env as target, or defaults to "production")
func parsePromote(promote, targetEnv string, skipReserved bool) (source, target string, err error) {
	// Try Unicode arrow
	if parts := strings.Split(promote, "→"); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	// Try ASCII arrow
	if parts := strings.Split(promote, "->"); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	// Single value: use as source, target from --env or default to production
	source = strings.TrimSpace(promote)
	if err := validateEnvName(source, skipReserved); err != nil {
		return "", "", fmt.Errorf("invalid source environment: %w", err)
	}
	target = targetEnv
	if target == "" {
		target = "production"
	}
	if err := validateEnvName(target, skipReserved); err != nil {
		return "", "", fmt.Errorf("invalid target environment: %w", err)
	}
	return source, target, nil
}

// --- deploy status subcommand ---

func newDeployStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [author/name]",
		Short: "Show per-environment deployment status",
		Long: `Display what version is currently deployed to each environment.

Shows all environment aliases, their versions, and when they were last
updated. Useful for verifying which version is live in staging vs production.`,
		Example: `  ff deploy status
  ff deploy status alice/my-fn
  ff deploy status --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeployStatus(args, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

type EnvDeployment struct {
	Env       string `json:"env"`
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func runDeployStatus(args []string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Fetch function metadata
	var fn struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := client.Get(fmt.Sprintf("/v1/registry/functions/%s/%s", author, name), &fn); err != nil {
		return fmt.Errorf("could not fetch function: %w", err)
	}

	// Fetch environment aliases
	var envs []EnvDeployment
	envPath := fmt.Sprintf("/v1/functions/%s/environments", fn.ID)
	if err := client.Get(envPath, &envs); err != nil {
		// Fall back to versions endpoint
		var versions []VersionInfo
		verPath := fmt.Sprintf("/v1/registry/functions/%s/%s/versions", author, name)
		if err2 := client.Get(verPath, &versions); err2 != nil {
			return fmt.Errorf("could not fetch deployment status: %w", err)
		}
		for _, v := range versions {
			if v.Active {
				envs = append(envs, EnvDeployment{
					Env:       "current",
					Version:   v.Version,
					UpdatedAt: v.DeployedAt,
				})
			}
		}
	}

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"function":     name,
			"author":       author,
			"environments": envs,
		})
		return nil
	}

	fmt.Printf("\n📦 %s/%s — deployment status\n", author, name)
	fmt.Println(strings.Repeat("─", 55))

	if len(envs) == 0 {
		fmt.Println("  No environment deployments found.")
		fmt.Printf("  Deploy with: ff deploy --env <name>\n")
		fmt.Println()
		return nil
	}

	for _, e := range envs {
		updatedAt := e.UpdatedAt
		if len(updatedAt) > 19 {
			updatedAt = updatedAt[:19]
		}
		if updatedAt == "" {
			updatedAt = "-"
		}
		marker := "  "
		if e.Env == "production" {
			marker = "🔴"
		} else if e.Env == "staging" {
			marker = "🟡"
		} else {
			marker = "🟢"
		}
		fmt.Printf("  %s %-16s  v%-12s  %s\n", marker, e.Env, e.Version, updatedAt)
	}

	fmt.Println()
	return nil
}

// --- deploy envs subcommand ---

func newDeployEnvsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "envs",
		Aliases: []string{"environments"},
		Short:   "List all environments for a function",
		Long:    `List all known environments (aliases) for a function.`,
		Example: `  ff deploy envs
  ff deploy envs alice/my-fn
  ff deploy envs --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeployStatus(args, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}
