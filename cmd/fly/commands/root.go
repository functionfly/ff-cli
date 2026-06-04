package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/version"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ff",
		Short: "FunctionFly CLI — publish functions to the global edge",
		Long: `ff is the FunctionFly developer CLI.

Go from idea → global API in under 60 seconds.

  ff login              Authenticate with FunctionFly
  ff init <name>        Scaffold a new function project
  ff dev                Run function locally
  ff publish            Publish function to the registry
  ff deploy --env       Publish and promote to staging or production
  ff deploy --canary N  Publish and start a canary at N% traffic
  ff canary             Manage canary deployments
  ff test               Test your deployed function
  ff health             Check deployed function health
  ff update <bump>      Bump function version
  ff stats              View usage statistics
  ff logs               Stream live execution logs
  ff rollback           Roll back to a previous version
  ff env                Manage environment variables
  ff secrets            Manage secrets
  ff whoami             Show current logged-in user
  ff logout             Clear stored credentials
  ff completion         Generate shell completion scripts`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Add --version flag (Cobra's built-in version support)
	root.Version = version.Short()

	// Add persistent flags for debug/verbose/trace modes
	// These are available to all subcommands
	root.PersistentFlags().BoolVar(&DebugMode, "debug", false, "Enable full debug output")
	root.PersistentFlags().BoolVarP(&VerboseMode, "verbose", "v", false, "Enable verbose API calls")
	root.PersistentFlags().BoolVar(&TraceMode, "trace", false, "Enable HTTP trace with request/response bodies")
	root.PersistentFlags().StringVarP(&OutputFormat, "format", "m", "table", "Output format: table, json")
	root.PersistentFlags().BoolVarP(&YesMode, "yes", "y", false, "Skip all confirmation prompts and answer yes automatically")

	// Set up persistent pre-run to handle debug mode
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if DebugMode {
			Debug("Debug mode enabled")
			Debug("Version: %s", version.Info())
		}
	}

	// Add version command
	root.AddCommand(NewVersionCmd())

	root.AddCommand(
		NewLoginCmd(),
		NewWhoamiCmd(),
		NewLogoutCmd(),
		NewAuthRefreshCmd(),
		NewConfigCmd(),
		NewSelfUpdateCmd(),
		NewInitCmd(),
		NewDevCmd(),
		NewPublishCmd(),
		NewDeployCmd(),
		NewPublishBatchCmd(),
		NewManifestCmd(),
		NewTestCmd(),
		NewUpdateCmd(),
		NewStatsCmd(),
		NewLogsCmd(),
		NewRollbackCmd(),
		NewHealthCmd(),
		NewCanaryCmd(),
		NewEnvCmd(),
		NewSecretsCmd(),
		NewScheduleCmd(),
		NewDreCmd(),
		NewCompletionCmd(root),
		NewCompletionsAliasCmd(root),
		NewDoctorCmd(),
		NewChangelogCmd(),
		BackendCmd(),
		FlypyCmd(),
		CompileCmd(),
	)

	return root
}

// NewVersionCmd creates the version command.
func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the ff CLI version",
		Long: `Show version information for the ff CLI.

This displays the semantic version, git commit hash, and build date.
Use this to verify which version of ff you have installed.`,
		Example: `  ff version
  ff version --short  # Show only version number
  ff version --json   # Output as JSON
  ff --format json version  # Honors global --format`,
		Run: func(cmd *cobra.Command, args []string) {
			short, _ := cmd.Flags().GetBool("short")
			asJSON, _ := cmd.Flags().GetBool("json")
			// Honor the global --format flag too: `ff --format json version` should print JSON.
			if !asJSON {
				asJSON = WantJSON()
			}
			if short {
				fmt.Println(version.Short())
			} else if asJSON {
				printJSON(map[string]interface{}{
					"version": version.Version,
					"commit":  version.Commit,
					"date":    version.Date,
				})
			} else {
				PrintVersion()
			}
		},
	}
	cmd.Flags().Bool("short", false, "Show only version number")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

// GetVersion returns the current CLI version string.
