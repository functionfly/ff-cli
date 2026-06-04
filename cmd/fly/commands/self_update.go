package commands

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/functionfly/ff-cli/internal/version"
	"github.com/spf13/cobra"
)

func NewSelfUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "self-update",
		Short: "Update the ff CLI to the latest version",
		Long:  `Automatically checks for a newer version of the ff CLI on GitHub and updates the binary in place.`,
		RunE:  runSelfUpdate,
	}
}

func runSelfUpdate(cmd *cobra.Command, args []string) error {
	currentVer, err := semver.NewVersion(version.Version)
	if err != nil {
		currentVer = semver.MustParse("0.0.0")
	}

	slug := selfupdate.ParseSlug("functionfly/ff-cli")

	latest, found, err := selfupdate.DetectLatest(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("error detecting latest version: %w", err)
	}
	if !found {
		return fmt.Errorf("no release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if latest.LessOrEqual(currentVer.String()) {
		fmt.Printf("ff is already up to date (version %s)\n", version.Version)
		return nil
	}

	fmt.Printf("Updating ff from %s to %s...\n", currentVer.String(), latest.Version())

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate executable path: %w", err)
	}

	if err := selfupdate.UpdateTo(context.Background(), latest.AssetURL, latest.AssetName, exe); err != nil {
		return fmt.Errorf("error updating binary: %w", err)
	}

	fmt.Printf("Successfully updated to version %s\n", latest.Version())
	return nil
}
