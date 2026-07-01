package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewDNACmd() *cobra.Command {
	var asJSON bool
	var mutations bool
	var variants bool
	cmd := &cobra.Command{
		Use:   "dna [author/name]",
		Short: "View function DNA, mutations, and variants",
		Long: `Inspect the "DNA" of a function — its unique fingerprint derived from
source code, configuration, dependencies, and runtime.

DNA tracks how a function evolves across publishes. Each publish creates
a new DNA strand. Mutations are changes between strands. Variants are
functions that share significant DNA (forks, templates, derived work).`,
		Example: `  ff dna
  ff dna alice/my-fn
  ff dna alice/my-fn --mutations
  ff dna alice/my-fn --variants
  ff dna alice/my-fn --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDNA(args, mutations, variants, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&mutations, "mutations", false, "Show mutation history between versions")
	cmd.Flags().BoolVar(&variants, "variants", false, "Show related functions (variants/forks)")
	return cmd
}

type DNAPayload struct {
	Function  string          `json:"function"`
	Strand    DNAStrand       `json:"strand"`
	History   []DNAStrand     `json:"history,omitempty"`
	Mutations []DNAMutation   `json:"mutations,omitempty"`
	Variants  []DNAVariant    `json:"variants,omitempty"`
}

type DNAStrand struct {
	Hash        string            `json:"hash"`
	Version     string            `json:"version"`
	Components  map[string]string `json:"components,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
}

type DNAMutation struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	Timestamp string `json:"timestamp,omitempty"`
}

type DNAVariant struct {
	Author    string  `json:"author"`
	Name      string  `json:"name"`
	Similarity float64 `json:"similarity"`
	Relation  string  `json:"relation"`
}

func runDNA(args []string, showMutations, showVariants, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	var dna DNAPayload
	dna.Function = author + "/" + name

	// Fetch current DNA strand
	strandPath := fmt.Sprintf("/v1/registry/functions/%s/%s/dna", author, name)
	if err := client.Get(strandPath, &dna.Strand); err != nil {
		return fmt.Errorf("could not fetch function DNA: %w", err)
	}

	// Fetch history
	histPath := fmt.Sprintf("/v1/registry/functions/%s/%s/dna/history", author, name)
	_ = client.Get(histPath, &dna.History)

	// Fetch mutations if requested
	if showMutations {
		mutPath := fmt.Sprintf("/v1/registry/functions/%s/%s/dna/mutations", author, name)
		_ = client.Get(mutPath, &dna.Mutations)
	}

	// Fetch variants if requested
	if showVariants {
		varPath := fmt.Sprintf("/v1/registry/functions/%s/%s/dna/variants", author, name)
		_ = client.Get(varPath, &dna.Variants)
	}

	if asJSON || WantJSON() {
		printJSON(dna)
		return nil
	}

	printDNA(dna, showMutations, showVariants)
	return nil
}

func printDNA(dna DNAPayload, showMutations, showVariants bool) {
	fmt.Printf("\n🧬 DNA: %s\n", dna.Function)
	fmt.Println(strings.Repeat("─", 58))

	strand := dna.Strand
	if strand.Hash == "" {
		fmt.Println("  No DNA data available.")
		fmt.Println()
		return
	}

	fmt.Printf("  Hash:     %s\n", formatDNAHash(strand.Hash))
	if strand.Version != "" {
		fmt.Printf("  Version:  %s\n", strand.Version)
	}
	if strand.CreatedAt != "" {
		fmt.Printf("  Created:  %s\n", strand.CreatedAt)
	}

	if len(strand.Components) > 0 {
		fmt.Printf("\n  Components:\n")
		keys := sortedKeys(strand.Components)
		for _, k := range keys {
			v := strand.Components[k]
			if len(v) > 40 {
				v = v[:37] + "..."
			}
			fmt.Printf("    %-16s %s\n", k, v)
		}
	}

	// History
	if len(dna.History) > 0 {
		fmt.Printf("\n  📜 Strand History (%d)\n", len(dna.History))
		for _, s := range dna.History {
			hash := formatDNAHash(s.Hash)
			ver := s.Version
			if ver == "" {
				ver = "-"
			}
			ts := s.CreatedAt
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s  v%-10s  %s\n", hash, ver, ts)
		}
	}

	// Mutations
	if showMutations {
		if len(dna.Mutations) == 0 {
			fmt.Printf("\n  🔬 No mutations recorded.\n")
		} else {
			fmt.Printf("\n  🔬 Mutations (%d)\n", len(dna.Mutations))
			for _, m := range dna.Mutations {
				kindIcon := mutationIcon(m.Kind)
				fmt.Printf("    %s %s  %s → %s\n", kindIcon, m.Kind, shortHashDNA(m.From), shortHashDNA(m.To))
				if m.Summary != "" {
					fmt.Printf("       %s\n", m.Summary)
				}
			}
		}
	}

	// Variants
	if showVariants {
		if len(dna.Variants) == 0 {
			fmt.Printf("\n  🧪 No variants found.\n")
		} else {
			fmt.Printf("\n  🧪 Variants (%d)\n", len(dna.Variants))
			for _, v := range dna.Variants {
				sim := fmt.Sprintf("%.0f%%", v.Similarity*100)
				fmt.Printf("    %-36s  %s  %s\n", v.Author+"/"+v.Name, sim, v.Relation)
			}
		}
	}

	fmt.Println()
}

func formatDNAHash(h string) string {
	if len(h) > 16 {
		return h[:8] + ".." + h[len(h)-6:]
	}
	return h
}

func shortHashDNA(h string) string {
	if len(h) > 10 {
		return h[:10] + ".."
	}
	return h
}

func mutationIcon(kind string) string {
	switch strings.ToLower(kind) {
	case "source":
		return "📝"
	case "config":
		return "⚙️"
	case "dependency":
		return "📦"
	case "runtime":
		return "🔧"
	case "major":
		return "💥"
	default:
		return "•"
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort for small maps
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
