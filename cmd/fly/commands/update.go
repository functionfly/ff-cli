package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update <bump>",
		Short:   "Bump the function version",
		Long:    "Bump the version in functionfly.jsonc.\n\nBump levels: patch, minor, major, or x.y.z",
		Example: "  ff update patch\n  ff update minor\n  ff update major\n  ff update 2.0.0",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bump := "patch"
			if len(args) > 0 {
				bump = args[0]
			} else if IsInteractive() {
				bump = PromptSelect("Version bump:", []string{"patch", "minor", "major"}, "patch")
			}
			return runUpdate(bump)
		},
	}
	return cmd
}

func runUpdate(bump string) error {
	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}
	current := manifest.Version
	newVersion, err := bumpVersion(current, bump)
	if err != nil {
		return err
	}
	manifest.Version = newVersion
	if err := SaveManifest("", manifest); err != nil {
		return fmt.Errorf("could not save manifest: %w", err)
	}
	fmt.Printf("✅ Version bumped\n")
	fmt.Printf("   %s → %s\n\n", current, newVersion)
	fmt.Printf("Run 'ff publish' to deploy the new version.\n")
	return nil
}

func bumpVersion(version, bump string) (string, error) {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version %q — expected semver (e.g. 1.0.0)", version)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid major version: %s", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid minor version: %s", parts[1])
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", fmt.Errorf("invalid patch version: %s", parts[2])
	}
	switch bump {
	case "patch":
		patch++
	case "minor":
		minor++
		patch = 0
	case "major":
		major++
		minor = 0
		patch = 0
	default:
		newParts := strings.Split(strings.TrimPrefix(bump, "v"), ".")
		if len(newParts) != 3 {
			return "", fmt.Errorf("invalid bump %q — use patch, minor, major, or a semver like 2.0.0", bump)
		}
		return strings.Join(newParts, "."), nil
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}
