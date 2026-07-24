package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantDiffCmd() *cobra.Command {
	var published bool
	var format string

	cmd := &cobra.Command{
		Use:     "diff [fci-file]",
		Short:   "Compare local FCI artifacts",
		Long:    "Compares two .fci artifacts and shows differences in manifest fields and payload SHA-256.",
		Example: "  ff implant diff my-implant.fci other-implant.fci\n  ff implant diff --format json",
		Args:    cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantDiff(args, format)
		},
	}

	cmd.Flags().BoolVarP(&published, "published", "p", false, "Compare against published version from registry")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json")

	return cmd
}

type implantDiffResult struct {
	Left  *implant.FCIArtifact   `json:"left"`
	Right *implant.FCIArtifact   `json:"right"`
	Diffs []implantDiffField    `json:"diffs"`
}

type implantDiffField struct {
	Field    string `json:"field"`
	LeftVal  string `json:"left_value"`
	RightVal string `json:"right_value"`
}

func runImplantDiff(args []string, format string) error {
	leftPath := args[0]
	rightPath := args[1]

	leftData, err := os.ReadFile(leftPath)
	if err != nil {
		return fmt.Errorf("failed to read left artifact: %w", err)
	}

	rightData, err := os.ReadFile(rightPath)
	if err != nil {
		return fmt.Errorf("failed to read right artifact: %w", err)
	}

	leftArt, err := implant.ParseFCIArtifact(leftData)
	if err != nil {
		return fmt.Errorf("failed to parse left artifact: %w", err)
	}

	rightArt, err := implant.ParseFCIArtifact(rightData)
	if err != nil {
		return fmt.Errorf("failed to parse right artifact: %w", err)
	}

	diffs := computeImplantDiffs(leftArt, rightArt)

	if format == "json" {
		result := implantDiffResult{
			Left:   leftArt,
			Right:  rightArt,
			Diffs:  diffs,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n🔍 Comparing %s ↔ %s\n", leftPath, rightPath)
	fmt.Println(strings.Repeat("─", 60))

	if len(diffs) == 0 {
		if leftArt.PayloadSHA == rightArt.PayloadSHA {
			fmt.Println("✅ Artifacts are identical")
		} else {
			fmt.Println("⚠️  Manifests match but payloads differ")
		}
	} else {
		fmt.Println("Differences found:")
		for _, d := range diffs {
			fmt.Printf("\n  %s:\n", d.Field)
			fmt.Printf("    - %s\n", d.LeftVal)
			fmt.Printf("    + %s\n", d.RightVal)
		}
	}

	if leftArt.PayloadSHA != rightArt.PayloadSHA {
		fmt.Printf("\n⚠️  Payload SHA-256 differs:\n")
		fmt.Printf("    %s: %s\n", leftPath, leftArt.PayloadSHA[:12])
		fmt.Printf("    %s: %s\n", rightPath, rightArt.PayloadSHA[:12])
	}

	fmt.Println()
	return nil
}

func computeImplantDiffs(left, right *implant.FCIArtifact) []implantDiffField {
	var diffs []implantDiffField

	if left.Manifest.Slug != right.Manifest.Slug {
		diffs = append(diffs, implantDiffField{Field: "slug", LeftVal: left.Manifest.Slug, RightVal: right.Manifest.Slug})
	}
	if left.Manifest.Runtime != right.Manifest.Runtime {
		diffs = append(diffs, implantDiffField{Field: "runtime", LeftVal: left.Manifest.Runtime, RightVal: right.Manifest.Runtime})
	}
	if left.Manifest.Size != right.Manifest.Size {
		diffs = append(diffs, implantDiffField{Field: "size", LeftVal: fmt.Sprintf("%d", left.Manifest.Size), RightVal: fmt.Sprintf("%d", right.Manifest.Size)})
	}
	if left.Manifest.YAML != right.Manifest.YAML {
		leftManifest, _ := implant.ParseManifest([]byte(left.Manifest.YAML))
		rightManifest, _ := implant.ParseManifest([]byte(right.Manifest.YAML))
		if leftManifest != nil && rightManifest != nil {
			if leftManifest.Version != rightManifest.Version {
				diffs = append(diffs, implantDiffField{Field: "version", LeftVal: leftManifest.Version, RightVal: rightManifest.Version})
			}
			if leftManifest.Name != rightManifest.Name {
				diffs = append(diffs, implantDiffField{Field: "name", LeftVal: leftManifest.Name, RightVal: rightManifest.Name})
			}
			if leftManifest.Category != rightManifest.Category {
				diffs = append(diffs, implantDiffField{Field: "category", LeftVal: leftManifest.Category, RightVal: rightManifest.Category})
			}
			if leftManifest.Deprecated != rightManifest.Deprecated {
				diffs = append(diffs, implantDiffField{Field: "deprecated", LeftVal: fmt.Sprintf("%v", leftManifest.Deprecated), RightVal: fmt.Sprintf("%v", rightManifest.Deprecated)})
			}
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Field < diffs[j].Field
	})

	return diffs
}
