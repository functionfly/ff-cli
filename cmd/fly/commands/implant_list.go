package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/functionfly/ff-cli/internal/implant"
)

func NewImplantListCmd() *cobra.Command {
	var local bool
	var format string

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List local FCI artifacts",
		Long:    "Lists all .fci artifacts in the current directory or specified directory, showing slug, runtime, and size.",
		Example: "  ff implant list\n  ff implant list --local ./dist\n  ff implant list --format json",
		RunE: func(_ *cobra.Command, args []string) error {
			return runImplantList(args, local, format)
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "List artifacts in current directory only")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json")

	return cmd
}

type implantListItem struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Runtime string `json:"runtime"`
	Size    int    `json:"size"`
	SHA256  string `json:"sha256"`
	ModTime string `json:"mod_time"`
}

func runImplantList(args []string, local bool, format string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".fci" {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	if len(files) == 0 {
		if local || len(args) > 0 {
			fmt.Printf("No .fci artifacts found in %s\n", dir)
		} else {
			fmt.Println("No .fci artifacts found in current directory")
		}
		return nil
	}

	var items []implantListItem
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		art, err := implant.ParseFCIArtifact(data)
		if err != nil {
			continue
		}

		info, _ := os.Stat(f)
		modTime := ""
		if info != nil {
			modTime = info.ModTime().Format(time.RFC3339)
		}

		items = append(items, implantListItem{
			Name:    filepath.Base(f),
			Slug:    art.Manifest.Slug,
			Runtime: art.Manifest.Runtime,
			Size:    len(data),
			SHA256:  art.Manifest.SHA256[:12],
			ModTime: modTime,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	if format == "json" {
		data, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n📦 FCI Artifacts (%d found)\n", len(items))
	fmt.Println(strings.Repeat("─", 70))

	for _, item := range items {
		sizeStr := formatImplantSize(item.Size)
		fmt.Printf("  %-30s %-20s %8s  %s\n", item.Name, item.Slug, sizeStr, item.ModTime[:10])
	}
	fmt.Println()
	return nil
}

func formatImplantSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}
