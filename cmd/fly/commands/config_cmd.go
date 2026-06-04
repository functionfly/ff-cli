package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
)

const configHelpLong = `Manage global ff CLI configuration.

Configuration precedence (highest first):
  1. Environment variables (FF_*)
  2. Global config file (~/.ff/config.yaml)
  3. Defaults

Environment variables (override config file):
  FF_API_URL         API base URL (e.g. https://api.functionfly.com or http://localhost:8080)
  FF_AUTH_SITE_URL   Dashboard / auth site base URL used in ff login links
                     (default: https://functionfly.com, or http://localhost:3000 when FF_API_URL is local)
  FF_API_TIMEOUT     Request timeout (e.g. 30s)
  FF_TOKEN           Bearer token (overrides stored credentials)
  FF_TELEMETRY       Set to 0, false, or no to disable telemetry
  FF_CONFIG          Path to config file (overrides ~/.ff/config.yaml)

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
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("invalid duration %q (e.g. 30s, 5m, 1h): %w", value, err)
		}
		cfg.API.Timeout = value
	case "telemetry.enabled":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean %q (use true/false, yes/no, 1/0): %w", value, err)
		}
		cfg.Telemetry.Enabled = b
	case "dev.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port %q: %w", value, err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("port %d out of range (1-65535)", port)
		}
		cfg.Dev.Port = port
	case "dev.watch":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean %q (use true/false, yes/no, 1/0): %w", value, err)
		}
		cfg.Dev.Watch = b
	case "dev.hot_reload":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean %q (use true/false, yes/no, 1/0): %w", value, err)
		}
		cfg.Dev.HotReload = b
	case "publish.auto_update":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean %q (use true/false, yes/no, 1/0): %w", value, err)
		}
		cfg.Publish.AutoUpdate = b
	default:
		return fmt.Errorf("unknown config key %q — supported: api.url, api.timeout, telemetry.enabled, dev.port, dev.watch, dev.hot_reload, publish.auto_update", key)
	}
	return nil
}

// parseBool accepts common truthy/falsy spellings: true/false, yes/no, 1/0, on/off.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("unrecognized boolean value")
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
