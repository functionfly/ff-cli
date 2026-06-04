package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// NewDreCmd returns the "ff dre" command and subcommands.
func NewDreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dre",
		Short: "DRE (Deterministic Reliable Execution) and FXCERT operations",
	}
	cmd.AddCommand(NewDreRegenerateBootstrapCmd())
	return cmd
}

// NewDreRegenerateBootstrapCmd returns "ff dre regenerate-bootstrap".
func NewDreRegenerateBootstrapCmd() *cobra.Command {
	var author string
	cmd := &cobra.Command{
		Use:   "regenerate-bootstrap",
		Short: "Regenerate bootstrap FXCERTs with the current node key (signed certs)",
		Long:  "Calls the API to delete existing bootstrap certificates and re-create them using the server's DRE_NODE_PRIVATE_KEY so the UI shows Node Key ID. Optional --author limits to one author (e.g. functionfly).",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewAPIClient()
			if err != nil {
				return err
			}
			path := "/v1/admin/registry/dre/regenerate-bootstrap"
			if author != "" {
				path += "?author=" + url.QueryEscape(author)
			}
			req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, nil)
			if err != nil {
				return err
			}
			if c.Token != "" {
				req.Header.Set("Authorization", "Bearer "+c.Token)
			}
			req.Header.Set("User-Agent", "ff-cli/1.0.0")
			// Regeneration can take a while; use a longer timeout (2 min)
			client := &http.Client{Timeout: 120 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()
			var result struct {
				Regenerated int    `json:"regenerated"`
				Message     string `json:"message"`
				Error       string `json:"error"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&result)
			if resp.StatusCode >= 400 {
				msg := result.Error
				if msg == "" {
					msg = result.Message
				}
				if msg == "" {
					msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
				return fmt.Errorf("%s", msg)
			}
			fmt.Println(strings.TrimSpace(result.Message))
			return nil
		},
	}
	cmd.Flags().StringVar(&author, "author", "", "Limit to functions by author (e.g. functionfly); omit for all")
	return cmd
}
