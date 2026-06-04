/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// flypyConfigCmd represents the flypy config command
var flypyConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage FlyPy configuration",
	Long: `Manage FlyPy configuration files and settings.

Examples:
  ff flypy config init    # Create a new flypy.yaml config file
  ff flypy config show    # Display current configuration`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// flypyConfigInitCmd initializes a new FlyPy configuration file
var flypyConfigInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new FlyPy configuration file",
	Long: `Creates a new flypy.yaml configuration file with default settings.

Examples:
  ff flypy config init`,
	Run: flypyConfigInitRun,
}

// flypyConfigShowCmd displays the current configuration
var flypyConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current FlyPy configuration",
	Long: `Displays the current FlyPy configuration from flypy.yaml or environment variables.

Examples:
  ff flypy config show`,
	Run: flypyConfigShowRun,
}

func init() {
	flypyCmd.AddCommand(flypyConfigCmd)
	flypyConfigCmd.AddCommand(flypyConfigInitCmd)
	flypyConfigCmd.AddCommand(flypyConfigShowCmd)
}

// FlyPyConfig represents the FlyPy configuration structure
type FlyPyConfig struct {
	// Build configuration
	Build struct {
		OutputDir string `yaml:"output_dir" json:"output_dir"`
		Mode      string `yaml:"mode" json:"mode"`
		Verbose   bool   `yaml:"verbose" json:"verbose"`
	} `yaml:"build" json:"build"`

	// Deploy configuration
	Deploy struct {
		Registry string   `yaml:"registry" json:"registry"`
		Public   bool     `yaml:"public" json:"public"`
		Tags     []string `yaml:"tags" json:"tags"`
	} `yaml:"deploy" json:"deploy"`

	// Local runtime configuration
	Local struct {
		Port  int    `yaml:"port" json:"port"`
		Host  string `yaml:"host" json:"host"`
		Watch bool   `yaml:"watch" json:"watch"`
	} `yaml:"local" json:"local"`
}

// LoadFlyPyConfig loads FlyPy configuration from flypy.yaml
func LoadFlyPyConfig() (*FlyPyConfig, error) {
	configPath := "flypy.yaml"

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return getDefaultConfig(), nil
	}

	// Read and parse config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config FlyPyConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveFlyPyConfig saves FlyPy configuration to flypy.yaml
func SaveFlyPyConfig(config *FlyPyConfig) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile("flypy.yaml", data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func getDefaultConfig() *FlyPyConfig {
	config := &FlyPyConfig{}

	// Set build defaults
	config.Build.OutputDir = "./dist"
	config.Build.Mode = "deterministic"
	config.Build.Verbose = false

	// Set deploy defaults
	config.Deploy.Registry = ""
	config.Deploy.Public = false
	config.Deploy.Tags = []string{}

	// Set local defaults
	config.Local.Port = 8080
	config.Local.Host = "localhost"
	config.Local.Watch = false

	return config
}

func flypyConfigInitRun(cmd *cobra.Command, args []string) {
	// Check if config file already exists
	if _, err := os.Stat("flypy.yaml"); err == nil {
		fmt.Fprintf(os.Stderr, "Error: flypy.yaml already exists\n")
		fmt.Fprintf(os.Stderr, "Use 'ff flypy config show' to view current configuration\n")
		os.Exit(1)
	}

	// Create default configuration
	config := getDefaultConfig()

	// Save configuration
	if err := SaveFlyPyConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create config file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Created flypy.yaml configuration file\n")
	fmt.Printf("\n")
	fmt.Printf("Edit the file to customize your FlyPy settings:\n")
	fmt.Printf("  build:    Compilation settings\n")
	fmt.Printf("  deploy:   Deployment settings  \n")
	fmt.Printf("  local:    Local runtime settings\n")
	fmt.Printf("\n")
	fmt.Printf("Use 'ff flypy config show' to view current settings\n")
}

func flypyConfigShowRun(cmd *cobra.Command, args []string) {
	config, err := LoadFlyPyConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("FlyPy Configuration:\n")
	fmt.Printf("\n")

	fmt.Printf("Build:\n")
	fmt.Printf("  Output Directory: %s\n", config.Build.OutputDir)
	fmt.Printf("  Mode:             %s\n", config.Build.Mode)
	fmt.Printf("  Verbose:          %t\n", config.Build.Verbose)
	fmt.Printf("\n")

	fmt.Printf("Deploy:\n")
	if config.Deploy.Registry != "" {
		fmt.Printf("  Registry: %s\n", config.Deploy.Registry)
	} else {
		fmt.Printf("  Registry: (not set - will use CLI config)\n")
	}
	fmt.Printf("  Public:   %t\n", config.Deploy.Public)
	if len(config.Deploy.Tags) > 0 {
		fmt.Printf("  Tags:     %v\n", config.Deploy.Tags)
	} else {
		fmt.Printf("  Tags:     (none)\n")
	}
	fmt.Printf("\n")

	fmt.Printf("Local:\n")
	fmt.Printf("  Host: %s\n", config.Local.Host)
	fmt.Printf("  Port: %d\n", config.Local.Port)
	fmt.Printf("  Watch: %t\n", config.Local.Watch)
	fmt.Printf("\n")

	// Show config file location
	configPath, _ := filepath.Abs("flypy.yaml")
	fmt.Printf("Configuration file: %s\n", configPath)
}
