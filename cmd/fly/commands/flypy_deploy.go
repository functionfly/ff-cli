/*
Copyright © 2026 FunctionFly
*/
package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/cmd/fly/telemetry"
	"github.com/functionfly/ff-cli/internal/cli"
	artifactPkg "github.com/functionfly/ff-cli/internal/flypy/artifact"
)

// flypyDeployCmd represents the flypy deploy command
var flypyDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy FlyPy artifact to FunctionFly registry",
	Long: `Deploys a compiled FlyPy artifact to the FunctionFly registry.

The artifact will be uploaded, verified for determinism, and made available
for execution through the FunctionFly API.

Examples:
  ff flypy deploy
  ff flypy deploy --artifact=./dist --registry=https://api.functionfly.com
  ff flypy deploy --dry-run`,
	Run: flypyDeployRun,
}

// flypyDeployFlags holds flags specific to the deploy command
var flypyDeployFlags struct {
	artifact  string
	registry  string
	dryRun    bool
	public    bool
	tags      []string
	publicKey string
}

func init() {
	flypyCmd.AddCommand(flypyDeployCmd)

	// Deploy-specific flags
	flypyDeployCmd.Flags().StringVarP(&flypyDeployFlags.artifact, "artifact", "a", "./dist", "Path to compiled artifact directory")
	flypyDeployCmd.Flags().StringVarP(&flypyDeployFlags.registry, "registry", "r", "", "Registry URL (defaults to configured API URL)")
	flypyDeployCmd.Flags().BoolVar(&flypyDeployFlags.dryRun, "dry-run", false, "Validate artifact without deploying")
	flypyDeployCmd.Flags().BoolVar(&flypyDeployFlags.public, "public", false, "Make function publicly accessible")
	flypyDeployCmd.Flags().StringSliceVarP(&flypyDeployFlags.tags, "tag", "t", []string{}, "Tags to apply to the function")
	flypyDeployCmd.Flags().StringVar(&flypyDeployFlags.publicKey, "public-key", "", "Public key for signature verification (base64 encoded)")
}

// flypyDeployRun implements the flypy deploy command
func flypyDeployRun(cmd *cobra.Command, args []string) {
	artifactPath := flypyDeployFlags.artifact

	cleanArtifactPath, err := cleanPath(artifactPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid artifact path: %v\n", err)
		os.Exit(1)
	}
	artifactPath = cleanArtifactPath

	// Validate artifact directory exists
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: artifact directory '%s' not found\n", artifactPath)
		fmt.Fprintf(os.Stderr, "Run 'ff flypy build' first to compile your function\n")
		os.Exit(1)
	}

	if flypyFlags.verbose {
		fmt.Printf("📦 Preparing FlyPy deployment...\n\n")
		fmt.Printf("   Artifact: %s\n", artifactPath)
		if flypyDeployFlags.dryRun {
			fmt.Printf("   Mode: dry-run (validation only)\n")
		} else {
			fmt.Printf("   Mode: deploy\n")
		}
		if flypyDeployFlags.public {
			fmt.Printf("   Visibility: public\n")
		} else {
			fmt.Printf("   Visibility: private\n")
		}
		if len(flypyDeployFlags.tags) > 0 {
			fmt.Printf("   Tags: %s\n", strings.Join(flypyDeployFlags.tags, ", "))
		}
		fmt.Printf("\n")
	}

	// Load and validate artifact
	artifact, err := loadArtifact(artifactPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load artifact: %v\n", err)
		os.Exit(1)
	}

	// Validate artifact completeness
	if err := validateArtifact(artifact, getPublicKey()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid artifact: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Artifact validation passed\n")

	if flypyDeployFlags.dryRun {
		fmt.Printf("✅ Dry run complete - artifact is valid for deployment\n")
		return
	}

	// Determine registry URL
	registryURL := flypyDeployFlags.registry
	if registryURL == "" {
		// Use API URL from global config or FF_API_URL
		apiURL := resolveAPIURL()
		if apiURL == "" {
			fmt.Fprintf(os.Stderr, "Error: no registry URL provided and no API URL configured\n")
			fmt.Fprintf(os.Stderr, "Either set --registry, FF_API_URL, or run 'ff login' first\n")
			os.Exit(1)
		}
		registryURL = apiURL
	}

	if flypyFlags.verbose {
		fmt.Printf("   Registry: %s\n", registryURL)
	}

	// Create deployment request
	deployReq, err := createDeploymentRequest(artifact, flypyDeployFlags.public, flypyDeployFlags.tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create deployment request: %v\n", err)
		os.Exit(1)
	}

	// Deploy to registry
	fmt.Printf("Deploying to registry...\n")

	// Load auth token from stored credentials
	token := ""
	if creds, err := LoadCredentials(); err == nil {
		token = creds.Token
	}

	telemetry.Emit(context.Background(), telemetry.Event{Kind: telemetry.EventDeployStart, Start: time.Now()})
	deployResp, err := deployToRegistry(registryURL, deployReq, token)
	if err != nil {
		telemetry.Emit(context.Background(), telemetry.Event{Kind: telemetry.EventDeployEnd, Status: "error", Error: err.Error(), Start: time.Now()})
		fmt.Fprintf(os.Stderr, "Error: deployment failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Deployment successful!\n")
	fmt.Printf("   Deployment ID: %s\n", deployResp.DeploymentID)
	fmt.Printf("   Version: %s\n", artifact.Manifest.Version)
	fmt.Printf("   Status: %s\n", deployResp.Status)

	if deployResp.Message != "" {
		fmt.Printf("   Message: %s\n", deployResp.Message)
	}

	telemetry.Emit(context.Background(), telemetry.Event{Kind: telemetry.EventDeployEnd, Status: "ok", Start: time.Now()})
	fmt.Printf("\n")
	fmt.Printf("Your function is now available at:\n")
	fmt.Printf("  %s/functions/%s\n", registryURL, artifact.Manifest.Name)
	fmt.Printf("\n")

	if flypyDeployFlags.public {
		fmt.Printf("Function is publicly accessible\n")
	} else {
		fmt.Printf("Function is private - configure access permissions as needed\n")
	}
}

// loadArtifact loads a FlyPy artifact from the specified directory
func loadArtifact(artifactPath string) (*artifactPkg.Artifact, error) {
	// Load manifest
	manifestPath := filepath.Join(artifactPath, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest artifactPkg.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Load Wasm module
	wasmPath := filepath.Join(artifactPath, "state_transition.wasm")
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Wasm module: %w", err)
	}

	// Load capability map
	capPath := filepath.Join(artifactPath, "capability.map")
	capData, err := os.ReadFile(capPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read capability map: %w", err)
	}

	var capMap artifactPkg.CapabilityMap
	if err := json.Unmarshal(capData, &capMap); err != nil {
		return nil, fmt.Errorf("failed to parse capability map: %w", err)
	}

	// Load determinism hash
	hashPath := filepath.Join(artifactPath, "determinism.hash")
	hashData, err := os.ReadFile(hashPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read determinism hash: %w", err)
	}

	// Load signature (optional)
	sigPath := filepath.Join(artifactPath, "signature.sig")
	var signature []byte
	if sigData, err := os.ReadFile(sigPath); err == nil {
		signature = sigData
	}

	return &artifactPkg.Artifact{
		Manifest:        &manifest,
		WasmModule:      wasmData,
		CapabilityMap:   &capMap,
		DeterminismHash: strings.TrimSpace(string(hashData)),
		Signature:       signature,
	}, nil
}

// validateArtifact performs comprehensive validation of the artifact
func validateArtifact(artifact *artifactPkg.Artifact, publicKeyB64 string) error {
	// Validate manifest
	if artifact.Manifest == nil {
		return fmt.Errorf("manifest is missing")
	}
	if artifact.Manifest.Name == "" {
		return fmt.Errorf("manifest missing function name")
	}
	if artifact.Manifest.Version == "" {
		return fmt.Errorf("manifest missing version")
	}

	// Validate Wasm module (basic check)
	if len(artifact.WasmModule) == 0 {
		return fmt.Errorf("Wasm module is empty")
	}

	// Check Wasm magic bytes
	if len(artifact.WasmModule) < 8 || string(artifact.WasmModule[0:4]) != "\x00asm" {
		return fmt.Errorf("invalid Wasm module (missing magic bytes)")
	}

	// Validate capability map
	if artifact.CapabilityMap == nil {
		return fmt.Errorf("capability map is missing")
	}

	// Validate determinism hash
	if artifact.DeterminismHash == "" {
		return fmt.Errorf("determinism hash is missing")
	}

	// Verify signature if present and public key is provided
	if len(artifact.Signature) > 0 {
		if publicKeyB64 == "" {
			return fmt.Errorf("artifact contains signature but no public key provided for verification")
		}

		// Decode the public key from base64
		publicKey, err := base64.StdEncoding.DecodeString(publicKeyB64)
		if err != nil {
			return fmt.Errorf("failed to decode public key: %w", err)
		}

		// Verify the signature
		if err := artifactPkg.VerifySignature(artifact, publicKey); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Verify determinism hash matches computed hash from artifact content
	computedHash, err := computeArtifactContentHash(artifact)
	if err != nil {
		return fmt.Errorf("failed to compute artifact content hash: %w", err)
	}

	if artifact.DeterminismHash != computedHash {
		return fmt.Errorf("determinism hash verification failed: expected %s, got %s", artifact.DeterminismHash, computedHash)
	}

	return nil
}

// createDeploymentRequest creates a deployment request from the artifact
func createDeploymentRequest(artifact *artifactPkg.Artifact, public bool, tags []string) (*cli.DeployRequest, error) {
	// Encode artifact files as base64
	manifestJSON, err := json.Marshal(artifact.Manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestB64 := base64Encode(manifestJSON)

	var sigB64 string
	if len(artifact.Signature) > 0 {
		sigB64 = base64Encode(artifact.Signature)
	}

	// Create provider config
	providerConfig := map[string]interface{}{
		"runtime":          "flypy",
		"deterministic":    true,
		"public":           public,
		"tags":             tags,
		"capability_map":   artifact.CapabilityMap,
		"determinism_hash": artifact.DeterminismHash,
	}

	if sigB64 != "" {
		providerConfig["signature"] = sigB64
	}

	return &cli.DeployRequest{
		Provider:       "flypy",
		Region:         "global",    // FlyPy functions are deterministic and can run anywhere
		Artifact:       manifestB64, // Use manifest as primary artifact identifier
		Routes:         []string{fmt.Sprintf("/functions/%s", artifact.Manifest.Name)},
		EnvVars:        map[string]string{},
		Secrets:        map[string]string{},
		ProviderConfig: providerConfig,
	}, nil
}

// deployToRegistry sends the deployment request to the registry.
func deployToRegistry(registryURL string, req *cli.DeployRequest, token string) (*cli.DeployResponse, error) {
	if u, err := url.Parse(registryURL); err != nil {
		return nil, fmt.Errorf("invalid registry URL %q: %w", registryURL, err)
	} else if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("registry URL must use http or https scheme: %s", registryURL)
	} else if u.Host == "" {
		return nil, fmt.Errorf("registry URL missing host: %s", registryURL)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:    90 * time.Second,
		},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	deployURL := strings.TrimSuffix(registryURL, "/") + "/api/v1/functions/deploy"
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, deployURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("deployment failed with status %d: %s", resp.StatusCode, string(respData))
	}

	var deployResp cli.DeployResponse
	if err := json.Unmarshal(respData, &deployResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &deployResp, nil
}

func parseRegistryURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("registry URL missing host: %s", raw)
	}
	host, _, portErr := net.SplitHostPort(u.Host)
	if portErr != nil && strings.Contains(portErr.Error(), "missing port") {
		if u.Scheme == "https" {
			u.Host = net.JoinHostPort(host, "443")
		} else if u.Scheme == "http" {
			u.Host = net.JoinHostPort(host, "80")
		}
	}
	return u, nil
}

// base64Encode encodes data to base64
func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// getPublicKey returns the public key from flag, environment variable, or empty string
func getPublicKey() string {
	// Check flag first (highest priority)
	if flypyDeployFlags.publicKey != "" {
		return flypyDeployFlags.publicKey
	}

	// Check environment variable
	if key := os.Getenv("FF_PUBLIC_KEY"); key != "" {
		return key
	}

	return ""
}

// computeArtifactContentHash computes a hash of the artifact's key components
// This ensures the artifact content hasn't been tampered with
func computeArtifactContentHash(artifact *artifactPkg.Artifact) (string, error) {
	hasher := sha256.New()

	// Include Wasm module
	hasher.Write(artifact.WasmModule)

	// Include manifest (canonical JSON)
	manifestJSON, err := json.Marshal(artifact.Manifest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}
	hasher.Write(manifestJSON)

	// Include capability map (canonical JSON)
	capMapJSON, err := json.Marshal(artifact.CapabilityMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal capability map: %w", err)
	}
	hasher.Write(capMapJSON)

	// Compute final hash
	hash := hasher.Sum(nil)
	return hex.EncodeToString(hash), nil
}

// resolveAPIURL returns the API URL from env/config, or empty string.
func resolveAPIURL() string {
	if url := os.Getenv("FF_API_URL"); url != "" {
		return url
	}
	if cfg, _ := LoadConfig(); cfg != nil {
		if cfg.API.URL != "" {
			return cfg.API.URL
		}
	}
	return "https://api.functionfly.com"
}
