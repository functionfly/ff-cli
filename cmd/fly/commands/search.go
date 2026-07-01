package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func NewSearchCmd() *cobra.Command {
	var asJSON bool
	var runtime string
	var limit int
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search the public function registry",
		Long: `Search the FunctionFly public marketplace for functions.

Returns matching functions ranked by relevance. Use --runtime to filter by
language runtime and --limit to control how many results are returned.`,
		Example: "  ff search image resize\n  ff search \"pdf merge\"\n  ff search csv --runtime python\n  ff search email --json --limit 20",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(strings.Join(args, " "), runtime, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Filter by runtime (javascript, python, rust, go, ruby, swift, kotlin, c, microvm, wasm, prism)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results (1-100)")
	return cmd
}

type SearchResult struct {
	ID          string  `json:"id"`
	Author      string  `json:"author"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Runtime     string  `json:"runtime"`
	Description string  `json:"description,omitempty"`
	Public      bool    `json:"public"`
	Calls       int64   `json:"calls,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total,omitempty"`
	Query   string         `json:"query"`
}

func runSearch(query, runtime string, limit int, asJSON bool) error {
	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{}
	params.Set("q", query)
	if runtime != "" {
		params.Set("runtime", runtime)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))

	path := "/v1/functions/search?" + params.Encode()

	var resp SearchResponse
	if err := client.Get(path, &resp); err != nil {
		pathV1 := "/v1/fx/search?" + params.Encode()
		if err2 := client.Get(pathV1, &resp); err2 != nil {
			return fmt.Errorf("search failed: %w", err)
		}
	}

	if asJSON || WantJSON() {
		printJSON(resp)
		return nil
	}

	if len(resp.Results) == 0 {
		fmt.Printf("No functions found for %q.\n", query)
		if runtime != "" {
			fmt.Printf("\nTry removing the --runtime filter or broadening your search.\n")
		}
		return nil
	}

	total := resp.Total
	if total == 0 {
		total = len(resp.Results)
	}
	fmt.Printf("\nResults for %q (%d)\n\n", query, total)

	for _, fn := range resp.Results {
		displayID := fn.Author + "/" + fn.Name
		desc := fn.Description
		if desc == "" {
			desc = "-"
		} else if len(desc) > 60 {
			desc = desc[:57] + "..."
		}

		meta := fn.Runtime
		if fn.Version != "" {
			meta += " v" + fn.Version
		}
		if fn.Calls > 0 {
			meta += "  " + formatNumber(fn.Calls) + " calls"
		}

		fmt.Printf("  %-36s  %s\n", displayID, meta)
		fmt.Printf("    %s\n\n", desc)
	}

	return nil
}
