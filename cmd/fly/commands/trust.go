package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func NewTrustCmd() *cobra.Command {
	var asJSON bool
	var verify bool
	cmd := &cobra.Command{
		Use:   "trust [author/name]",
		Short: "View trust scores and verify function integrity",
		Long: `Display the trust score for a function and optionally verify its
integrity against the deployed version.

The trust score aggregates multiple factors: code quality, dependency
health, vulnerability scan results, runtime safety, and deployment
history into a single 0–100 rating with a letter grade.

With --verify, the local source hash is compared against the deployed
version to detect tampering or drift.`,
		Example: `  ff trust
  ff trust alice/my-fn
  ff trust alice/my-fn --verify
  ff trust alice/my-fn --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrust(args, verify, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify local source integrity against deployed version")
	return cmd
}

type TrustDetail struct {
	Score       float64            `json:"score"`
	Grade       string             `json:"grade,omitempty"`
	Factors     map[string]float64 `json:"factors,omitempty"`
	LastChecked string             `json:"last_checked,omitempty"`
	Signature   string             `json:"signature,omitempty"`
	SourceHash  string             `json:"source_hash,omitempty"`
	Verified    bool               `json:"verified,omitempty"`
}

type IntegrityResult struct {
	LocalHash    string `json:"local_hash"`
	RemoteHash   string `json:"remote_hash"`
	Match        bool   `json:"match"`
	Algorithm    string `json:"algorithm"`
	Verified     bool   `json:"verified"`
	SignatureOK  bool   `json:"signature_ok,omitempty"`
}

func runTrust(args []string, verify, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Fetch trust score
	var trust TrustDetail
	trustPath := fmt.Sprintf("/v1/registry/functions/%s/%s/trust", author, name)
	if err := client.Get(trustPath, &trust); err != nil {
		return fmt.Errorf("could not fetch trust score: %w", err)
	}

	// Fetch deployed hash for verification
	var integrity *IntegrityResult
	if verify {
		integrity = &IntegrityResult{Algorithm: "sha256"}

		// Get remote hash
		var deployed struct {
			SourceHash string `json:"source_hash,omitempty"`
			Hash       string `json:"hash,omitempty"`
			Signature  string `json:"signature,omitempty"`
		}
		hashPath := fmt.Sprintf("/v1/registry/functions/%s/%s/hash", author, name)
		if err := client.Get(hashPath, &deployed); err == nil {
			remoteHash := deployed.SourceHash
			if remoteHash == "" {
				remoteHash = deployed.Hash
			}
			integrity.RemoteHash = remoteHash
			integrity.SignatureOK = deployed.Signature != ""
			trust.Signature = deployed.Signature
		}

		// Compute local hash
		localHash, err := hashLocalSourceSHA256()
		if err == nil {
			integrity.LocalHash = localHash
		}

		if integrity.LocalHash != "" && integrity.RemoteHash != "" {
			integrity.Match = strings.EqualFold(integrity.LocalHash, integrity.RemoteHash)
			integrity.Verified = integrity.Match
		}
	}

	if asJSON || WantJSON() {
		out := map[string]interface{}{
			"function": author + "/" + name,
			"trust":    trust,
		}
		if integrity != nil {
			out["integrity"] = integrity
		}
		printJSON(out)
		return nil
	}

	printTrust(author, name, trust, integrity)
	return nil
}

func printTrust(author, name string, trust TrustDetail, integrity *IntegrityResult) {
	grade := trust.Grade
	if grade == "" {
		grade = scoreToGradeTrust(trust.Score)
	}

	gradeIcon := gradeIcon(grade)

	fmt.Printf("\n%s Trust: %s/%s\n", gradeIcon, author, name)
	fmt.Println(strings.Repeat("─", 55))

	fmt.Printf("  Score:     %.0f/100  (%s)\n", trust.Score, grade)
	fmt.Printf("  Rating:    %s\n", ratingLabel(trust.Score))

	if len(trust.Factors) > 0 {
		fmt.Printf("\n  Factors:\n")
		// Sort factor names for consistent output
		keys := make([]string, 0, len(trust.Factors))
		for k := range trust.Factors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := trust.Factors[k]
			bar := factorBar(v)
			fmt.Printf("    %-20s %s %.0f\n", k, bar, v)
		}
	}

	if trust.LastChecked != "" {
		fmt.Printf("\n  Last checked: %s\n", trust.LastChecked)
	}

	if trust.Signature != "" {
		sig := trust.Signature
		if len(sig) > 48 {
			sig = sig[:48] + "..."
		}
		fmt.Printf("  Signature:  %s\n", sig)
	}

	// Integrity verification section
	if integrity != nil {
		fmt.Printf("\n🔒 Integrity Verification\n")
		fmt.Println(strings.Repeat("─", 55))
		fmt.Printf("  Algorithm:   %s\n", integrity.Algorithm)

		if integrity.LocalHash != "" {
			fmt.Printf("  Local hash:  %s\n", shortHash(integrity.LocalHash))
		} else {
			fmt.Printf("  Local hash:  (no local source found)\n")
		}

		if integrity.RemoteHash != "" {
			fmt.Printf("  Remote hash: %s\n", shortHash(integrity.RemoteHash))
		} else {
			fmt.Printf("  Remote hash: (not available)\n")
		}

		if integrity.Verified {
			fmt.Printf("\n  ✅ Integrity verified — local source matches deployed version\n")
		} else if integrity.LocalHash != "" && integrity.RemoteHash != "" {
			fmt.Printf("\n  ❌ MISMATCH — local source differs from deployed version\n")
			fmt.Printf("     Run 'ff diff' to see what changed.\n")
		}

		if integrity.SignatureOK {
			fmt.Printf("  ✅ Cryptographic signature valid\n")
		}
	}

	fmt.Println()
}

// scoreToGradeTrust converts a numeric score to a letter grade.
func scoreToGradeTrust(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

func gradeIcon(grade string) string {
	switch grade {
	case "A":
		return "🟢"
	case "B":
		return "🔵"
	case "C":
		return "🟡"
	case "D":
		return "🟠"
	default:
		return "🔴"
	}
}

func ratingLabel(score float64) string {
	switch {
	case score >= 95:
		return "Excellent"
	case score >= 85:
		return "Good"
	case score >= 70:
		return "Fair"
	case score >= 50:
		return "Needs improvement"
	default:
		return "Poor"
	}
}

func factorBar(score float64) string {
	filled := int(score / 5) // 0–100 → 0–20 chars
	if filled > 20 {
		filled = 20
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)
}

func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16] + "..." + h[len(h)-8:]
	}
	return h
}

// hashLocalSourceSHA256 computes a SHA-256 hash of all source files
// in the current directory.
func hashLocalSourceSHA256() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext == ".js" || ext == ".ts" || ext == ".py" || ext == ".rs" || ext == ".go" ||
			name == "functionfly.jsonc" || name == "functionfly.json" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		hasher.Write(data)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
