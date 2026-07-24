package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantPublishCmd() *cobra.Command {
	var fciPath string
	var sigPath string
	var publisherKeyID string
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:     "publish [fci-file]",
		Short:   "Publish an FCI artifact to the registry",
		Long:    "Publishes an .fci artifact to the FunctionFly registry via multipart POST to /api/capabilities/ffpkg/publish.",
		Example: "  ff implant publish my-implant.fci\n  ff implant publish --publisher-key-id key_abc123 my-implant.fci\n  ff implant publish --dry-run",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantPublish(args, fciPath, sigPath, publisherKeyID, dryRun, force)
		},
	}

	cmd.Flags().StringVar(&fciPath, "fci", "", "Path to .fci artifact (default: *.fci in current directory)")
	cmd.Flags().StringVar(&sigPath, "sig", "", "Path to .sig signature file (default: <fci>.sig)")
	cmd.Flags().StringVar(&publisherKeyID, "publisher-key-id", "", "Publisher key ID for signing")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and show what would be published without publishing")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompts")

	return cmd
}

func runImplantPublish(args []string, fciPath, sigPath, publisherKeyID string, dryRun, force bool) error {
	creds, err := requireAuth()
	if err != nil {
		return err
	}

	if fciPath == "" {
		if len(args) > 0 {
			fciPath = args[0]
		} else {
			fciPath, err = findFCIArtifact()
			if err != nil {
				return fmt.Errorf("no .fci artifact specified and none found in current directory\n   → Specify with --fci or as argument")
			}
		}
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

	if sigPath == "" {
		sigPath = fciPath + ".sig"
	}

	sigData := []byte{}
	if _, err := os.Stat(sigPath); err == nil {
		sigData, err = os.ReadFile(sigPath)
		if err != nil {
			return fmt.Errorf("failed to read signature file: %w", err)
		}
	}

	if publisherKeyID == "" {
		publisherKeyID = os.Getenv("FF_PUBLISHER_KEY_ID")
		if publisherKeyID == "" {
			return fmt.Errorf("publisher key ID is required\n   → Set --publisher-key-id or FF_PUBLISHER_KEY_ID environment variable")
		}
	}

	manifest, err := implant.ParseManifest([]byte(art.Manifest.YAML))
	if err != nil {
		return fmt.Errorf("failed to parse implant manifest: %w", err)
	}

	if dryRun {
		return printDryRun(art, manifest, len(fciData), sigData)
	}

	if !force && !YesMode && IsInteractive() {
		confirmed := PromptConfirm(fmt.Sprintf("Publish %s@%s?", manifest.ID, manifest.Version), true)
		if !confirmed {
			fmt.Println("Publish cancelled.")
			return nil
		}
	}

	result, err := publishFCIArtifact(creds.Token, art, fciData, sigData, publisherKeyID)
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	fmt.Printf("\n✅ Published %s@%s\n", manifest.ID, manifest.Version)
	fmt.Printf("   Slug:    %s\n", result.Slug)
	fmt.Printf("   Version: %s\n", result.Version)
	fmt.Printf("   Status:  %s\n", result.Status)
	if result.PayloadSHA256 != "" {
		fmt.Printf("   SHA256:  %s\n", result.PayloadSHA256[:12])
	}
	return nil
}

func findFCIArtifact() (string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".fci" {
			return entry.Name(), nil
		}
	}
	return "", fmt.Errorf("no .fci artifact found in current directory")
}

func printDryRun(art *implant.FCIArtifact, manifest *implant.ImplantManifest, artifactSize int, sigData []byte) error {
	fmt.Printf("\n📋 Dry run - would publish:\n")
	fmt.Printf("   Slug:      %s\n", manifest.ID)
	fmt.Printf("   Name:      %s\n", manifest.Name)
	fmt.Printf("   Version:   %s\n", manifest.Version)
	fmt.Printf("   Category:  %s\n", manifest.Category)
	fmt.Printf("   Runtime:   %s\n", art.Manifest.Runtime)
	fmt.Printf("   SHA256:    %s\n", art.Manifest.SHA256[:12])
	fmt.Printf("   Size:      %d bytes\n", artifactSize)
	fmt.Printf("   Signature: %s\n", boolStr(len(sigData) > 0))
	if len(manifest.Actions) > 0 {
		fmt.Printf("   Actions:   %d\n", len(manifest.Actions))
	}
	fmt.Println("\nRun without --dry-run to publish.")
	return nil
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

type FFpkgPublishResponse struct {
	ID            string `json:"id,omitempty"`
	Slug          string `json:"slug"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	PayloadSHA256 string `json:"payload_sha256"`
	PayloadSize   int    `json:"payload_size"`
	SignatureOK   bool   `json:"signature_verified"`
	PublisherKeyID string `json:"publisher_key_id,omitempty"`
}

func publishFCIArtifact(token string, art *implant.FCIArtifact, fciData, sigData []byte, publisherKeyID string) (*FFpkgPublishResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	writer.WriteField("slug", art.Manifest.Slug)
	writer.WriteField("version", extractVersion(art.Manifest.YAML))
	writer.WriteField("publisher_key_id", publisherKeyID)
	writer.WriteField("algorithm", "hmac-sha256")

	fciPart, _ := writer.CreateFormFile("artifact", "artifact.fci")
	fciPart.Write(fciData)

	if len(sigData) > 0 {
		sigPart, _ := writer.CreateFormFile("signature", "SIGNATURE")
		sigPart.Write(sigData)
	}

	writer.Close()

	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	apiURL := cfg.API.URL
	if apiURL == "" {
		apiURL = "https://api.functionfly.com"
	}

	req, err := http.NewRequest("POST", apiURL+"/api/capabilities/ffpkg/publish", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("publish failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result FFpkgPublishResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func extractVersion(yamlContent string) string {
	manifest, err := implant.ParseManifest([]byte(yamlContent))
	if err != nil {
		return "unknown"
	}
	return manifest.Version
}
