package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
)

const configHelpLong = `Manage global ff CLI configuration.

Configuration precedence (highest first):
  1. Environment variables (FF_*)
  2. Global config file (~/.ff/config.yaml)
  3. Defaults

Environment variables (override config file):
  FF_API_URL       API base URL (e.g. https://api.functionfly.com or http://localhost:8080)
  FF_API_TIMEOUT  Request timeout (e.g. 30s)
  FF_DEV_EMAIL    Email for dev login (ff login --dev)
  FF_DEV_PASSWORD Password for dev login
  FF_DEV_LOGIN=1  Force dev email/password login
  FF_TOKEN        Bearer token (overrides stored credentials)
  FF_TELEMETRY    Set to 0, false, or no to disable telemetry
  FF_CONFIG       Path to config file (overrides ~/.ff/config.yaml)

Use "ff config" or "ff config view" to show current config and path.
Use "ff config reset" to restore defaults (removes or overwrites config file).`

// NewConfigCmd returns the config command and its subcommands (view, set, reset).
func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or reset global CLI configuration",
		Long:  configHelpLong,
		Example: `  ff config
  ff config view
  ff config set api.url=https://api.example.com
  ff config reset`,
		RunE: runConfigView, // "ff config" with no subcommand runs view
	}
	cmd.AddCommand(newConfigViewCmd(), newConfigShowCmd(), newConfigSetCmd(), newConfigResetCmd())
	return cmd
}

func newConfigViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Show config file path and current configuration",
		Long:  "Prints the path to the global config file and its contents (or 'using defaults' if the file does not exist). Environment overrides are applied when the CLI runs; this shows the merged view from file + env.",
		RunE:  runConfigView,
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "show",
		Short:  "Alias for 'view' — show current configuration",
		RunE:   runConfigView,
		Hidden: true,
	}
}

func runConfigView(cmd *cobra.Command, args []string) error {
	path, err := ConfigPath()
	if err != nil {
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not determine config path: %v", err))
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	fmt.Println("Config path:", path)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("(file does not exist — using defaults)")
	}
	fmt.Println()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func newConfigResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset config to defaults",
		Long:  "Writes default configuration to the config file (or removes it so defaults are used). Prints the config path.",
		RunE:  runConfigReset,
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY=VALUE [KEY=VALUE...]",
		Short: "Set one or more config values",
		Long: `Set config values in the global config file.
Keys use dot notation to set nested values:

  ff config set api.url=https://api.example.com
  ff config set api.timeout=60s
  ff config set telemetry.enabled=false
  ff config set dev.port=8787 dev.watch=true

Use "ff config view" to see all available keys and current values.`,
		Example: `  ff config set api.url=https://api.example.com
  ff config set api.timeout=30s telemetry.enabled=false`,
		Args: cobra.MinimumNArgs(1),
		RunE: runConfigSet,
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not load config: %v", err))
	}

	setCount := 0
	for _, pair := range args {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return NewCLIError(fmt.Errorf("invalid format %q — expected KEY=VALUE", pair), ExitCodeValidationError, "")
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return NewCLIError(fmt.Errorf("empty key in %q", pair), ExitCodeValidationError, "")
		}
		if err := setConfigKey(cfg, key, value); err != nil {
			return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not set %q: %v", key, err))
		}
		setCount++
	}

	if err := SaveConfig(cfg); err != nil {
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not save config: %v", err))
	}

	fmt.Printf("Set %d value(s) in %s\n", setCount, cfg.API.URL)
	path, _ := ConfigPath()
	fmt.Printf("Config file: %s\n", path)
	return nil
}

// setConfigKey sets a top-level config key.
// Supported keys: api.url, api.timeout, telemetry.enabled, dev.port, dev.watch, dev.hot_reload, publish.auto_update.
func setConfigKey(cfg *GlobalConfig, key, value string) error {
	switch key {
	case "api.url":
		cfg.API.URL = value
	case "api.timeout":
		cfg.API.Timeout = value
	case "telemetry.enabled":
		cfg.Telemetry.Enabled = value != "0" && value != "false" && value != "no"
	case "dev.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port %q: %w", value, err)
		}
		cfg.Dev.Port = port
	case "dev.watch":
		cfg.Dev.Watch = value == "1" || value == "true" || value == "yes"
	case "dev.hot_reload":
		cfg.Dev.HotReload = value == "1" || value == "true" || value == "yes"
	case "publish.auto_update":
		cfg.Publish.AutoUpdate = value == "1" || value == "true" || value == "yes"
	default:
		return fmt.Errorf("unknown config key %q — supported: api.url, api.timeout, telemetry.enabled, dev.port, dev.watch, dev.hot_reload, publish.auto_update", key)
	}
	return nil
}

func runConfigReset(cmd *cobra.Command, args []string) error {
	path, err := ConfigPath()
	if err != nil {
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not determine config path: %v", err))
	}
	if err := SaveConfig(DefaultConfig()); err != nil {
		return NewCLIError(err, ExitCodeConfigError, fmt.Sprintf("could not reset config: %v\n   → Check permissions or FF_CONFIG", err))
	}
	fmt.Printf("Config reset to defaults.\nConfig file: %s\n", path)
	return nil
}
