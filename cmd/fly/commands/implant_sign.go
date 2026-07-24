package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantSignCmd() *cobra.Command {
	var keyEnvVar string
	var keyFile string
	var outputPath string

	cmd := &cobra.Command{
		Use:     "sign [fci-file]",
		Short:   "Sign an FCI artifact manifest with HMAC-SHA256",
		Long:    "Signs the manifest JSON of an .fci artifact using HMAC-SHA256. The signature is written to a .sig file alongside the artifact.",
		Example: "  ff implant sign my-implant.fci\n  ff implant sign --key-env FF_CAPABILITY_SIGNING_KEY my-implant.fci\n  ff implant sign --key-file ./signing-key.txt my-implant.fci",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantSign(args, keyEnvVar, keyFile, outputPath)
		},
	}

	cmd.Flags().StringVar(&keyEnvVar, "key-env", "FF_CAPABILITY_SIGNING_KEY", "Environment variable containing the signing key")
	cmd.Flags().StringVar(&keyFile, "key-file", "", "File containing the signing key (takes precedence over --key-env)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for .sig file (default: <artifact>.sig)")

	return cmd
}

func runImplantSign(args []string, keyEnvVar, keyFile, outputPath string) error {
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
		return fmt.Errorf("failed to parse .fci artifact: %w", err)
	}

	signingKey := ""
	if keyFile != "" {
		keyData, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("failed to read signing key file: %w", err)
		}
		signingKey = strings.TrimSpace(string(keyData))
	} else {
		signingKey = os.Getenv(keyEnvVar)
		if signingKey == "" {
			return fmt.Errorf("signing key is empty\n   → Set %s environment variable or use --key-file", keyEnvVar)
		}
	}

	signature, err := implant.SignFCIArtifact(art.ManifestJSON, signingKey)
	if err != nil {
		return fmt.Errorf("failed to sign artifact: %w", err)
	}

	if outputPath == "" {
		outputPath = fciPath + ".sig"
	}

	if err := os.WriteFile(outputPath, []byte(signature), 0600); err != nil {
		return fmt.Errorf("failed to write signature file: %w", err)
	}

	manifest := art.Manifest
	fmt.Printf("\n✅ Signed %s\n", fciPath)
	fmt.Printf("   Slug:    %s\n", manifest.Slug)
	fmt.Printf("   Runtime: %s\n", manifest.Runtime)
	fmt.Printf("   SHA256:  %s\n", manifest.SHA256[:12])
	fmt.Printf("   Signature: %s\n\n", outputPath)
	fmt.Println("Next step:")
	fmt.Println("  ff implant publish   # Publish to the registry")
	return nil
}

// parseManifestJSON parses raw JSON bytes into a map.
//nolint:unused // Reserved for future use with manifest validation
func parseManifestJSON(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
