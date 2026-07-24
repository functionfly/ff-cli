package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantValidateCmd() *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:     "validate [fci-file]",
		Short:   "Validate an FCI artifact for integrity and correctness",
		Long:    "Parses an .fci artifact, verifies the manifest structure, checks payload SHA-256, and validates the manifest YAML against the ImplantManifest schema.",
		Example: "  ff implant validate my-implant.fci\n  ff implant validate --strict my-implant.fci",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantValidate(args, strict)
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "Fail on warnings (non-zero exit on any validation issue)")

	return cmd
}

func runImplantValidate(args []string, strict bool) error {
	fciPath := "*.fci"
	if len(args) > 0 {
		fciPath = args[0]
	}

	if _, err := os.Stat(fciPath); os.IsNotExist(err) {
		return fmt.Errorf("FCI artifact not found: %s\n   → Run 'ff implant build' first", fciPath)
	}

	fciData, err := os.ReadFile(fciPath)
	if err != nil {
		return fmt.Errorf("failed to read .fci artifact: %w", err)
	}

	art, err := implant.ParseFCIArtifact(fciData)
	if err != nil {
		return fmt.Errorf("❌ Parse error: %w", err)
	}

	fmt.Printf("✅ Valid .fci artifact: %s\n", fciPath)
	fmt.Printf("   Slug:       %s\n", art.Manifest.Slug)
	fmt.Printf("   Runtime:    %s\n", art.Manifest.Runtime)
	fmt.Printf("   Size:       %d bytes\n", art.Manifest.Size)
	fmt.Printf("   PayloadSHA: %s\n", art.PayloadSHA[:12])

	manifest, err := implant.ParseManifest([]byte(art.Manifest.YAML))
	if err != nil {
		return fmt.Errorf("❌ Manifest parse error: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("❌ Manifest validation error: %w", err)
	}

	fmt.Printf("✅ Manifest valid: %s@%s\n", manifest.ID, manifest.Version)
	fmt.Printf("   Category: %s\n", manifest.Category)

	if manifest.Deprecated {
		fmt.Printf("⚠️  WARNING: This implant is deprecated")
		if manifest.SunsetDate != "" {
			fmt.Printf(" (sunset date: %s)", manifest.SunsetDate)
		}
		fmt.Println()
	}

	if strict && manifest.Deprecated {
		return fmt.Errorf("strict mode: implant is deprecated")
	}

	fmt.Println("\n✅ All validations passed")
	return nil
}
