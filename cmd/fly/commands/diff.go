package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func NewDiffCmd() *cobra.Command {
	var asJSON bool
	var env string
	cmd := &cobra.Command{
		Use:   "diff [author/name]",
		Short: "Compare local vs deployed state",
		Long: `Show what would change if you publish now — like terraform plan for
your function.

Compares the local functionfly.jsonc manifest and source files against the
currently deployed version. Shows added, changed, and removed fields,
plus a source code hash comparison.`,
		Example: `  ff diff
  ff diff alice/my-fn
  ff diff --env production
  ff diff --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args, env, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&env, "env", "", "Compare against a specific environment (any name)")
	return cmd
}

type DiffField struct {
	Field   string      `json:"field"`
	Local   interface{} `json:"local"`
	Remote  interface{} `json:"remote"`
	Changed bool        `json:"changed"`
}

type DiffResult struct {
	Function    string      `json:"function"`
	LocalVer    string      `json:"local_version"`
	RemoteVer   string      `json:"remote_version,omitempty"`
	SourceMatch bool        `json:"source_match"`
	SourceHash  string      `json:"source_hash,omitempty"`
	Changes     []DiffField `json:"changes"`
	HasChanges  bool        `json:"has_changes"`
}

func runDiff(args []string, env string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}

	manifest, err := LoadManifest("")
	if err != nil {
		return err
	}

	client, err := NewAPIClient()
	if err != nil {
		return err
	}

	// Fetch deployed function metadata
	var remote FunctionMetadata
	fnPath := fmt.Sprintf("/v1/registry/functions/%s/%s", author, name)
	remoteErr := client.Get(fnPath, &remote)

	result := DiffResult{
		Function: author + "/" + name,
		LocalVer: manifest.Version,
	}
	if remoteErr == nil {
		result.RemoteVer = remote.Version
	}

	// Compare manifest fields
	result.Changes = compareManifest(manifest, &remote, remoteErr == nil)

	// Source hash
	localHash, hashErr := hashLocalSource()
	if hashErr == nil {
		result.SourceHash = localHash
	}

	// Fetch remote source hash if available
	if remoteErr == nil {
		var deployed struct {
			SourceHash string `json:"source_hash,omitempty"`
			Hash       string `json:"hash,omitempty"`
		}
		hashPath := fmt.Sprintf("/v1/registry/functions/%s/%s/hash", author, name)
		if err := client.Get(hashPath, &deployed); err == nil {
			remoteHash := deployed.SourceHash
			if remoteHash == "" {
				remoteHash = deployed.Hash
			}
			if remoteHash != "" && localHash != "" {
				result.SourceMatch = strings.EqualFold(localHash, remoteHash)
				if !result.SourceMatch {
					result.Changes = append(result.Changes, DiffField{
						Field:   "source_hash",
						Local:   localHash[:12],
						Remote:  remoteHash[:12],
						Changed: true,
					})
				}
			}
		}
	}

	// Filter to only changed fields
	changed := result.Changes[:0]
	for _, c := range result.Changes {
		if c.Changed {
			changed = append(changed, c)
		}
	}
	result.Changes = changed
	result.HasChanges = len(changed) > 0 || !result.SourceMatch

	if asJSON || WantJSON() {
		printJSON(result)
		return nil
	}

	printDiff(result)
	return nil
}

func compareManifest(local *Manifest, remote *FunctionMetadata, hasRemote bool) []DiffField {
	var fields []DiffField

	if !hasRemote {
		fields = append(fields, DiffField{Field: "status", Local: "new (not deployed)", Remote: nil, Changed: true})
		return fields
	}

	fields = append(fields, diffField("version", local.Version, remote.Version))
	fields = append(fields, diffField("runtime", local.Runtime, remote.Runtime))
	fields = append(fields, diffField("public", local.Public, remote.Public))
	fields = append(fields, diffField("description", local.Description, remote.Description))
	fields = append(fields, diffField("deterministic", local.Deterministic, remote.Deterministic))
	fields = append(fields, diffField("cache_ttl", local.CacheTTL, remote.CacheTTL))
	fields = append(fields, diffField("timeout_ms", local.TimeoutMS, remote.TimeoutMS))
	fields = append(fields, diffField("memory_mb", local.MemoryMB, remote.MemoryMB))
	fields = append(fields, compareEnv(local.Env, remote.Env)...)

	return fields
}

// compareDeps compares local and remote dependencies.
//nolint:unused // Deps comparison pending remote API support
func diffField(name string, local, remote interface{}) DiffField {
	localJSON, _ := json.Marshal(local)
	remoteJSON, _ := json.Marshal(remote)
	return DiffField{
		Field:   name,
		Local:   local,
		Remote:  remote,
		Changed: string(localJSON) != string(remoteJSON),
	}
}

func compareEnv(local, remote map[string]string) []DiffField {
	var fields []DiffField
	if local == nil {
		local = map[string]string{}
	}
	if remote == nil {
		remote = map[string]string{}
	}
	allKeys := make(map[string]bool)
	for k := range local {
		allKeys[k] = true
	}
	for k := range remote {
		allKeys[k] = true
	}
	for k := range allKeys {
		lv, lok := local[k]
		rv, rok := remote[k]
		switch {
		case lok && !rok:
			fields = append(fields, DiffField{Field: "env." + k, Local: lv, Remote: nil, Changed: true})
		case !lok && rok:
			fields = append(fields, DiffField{Field: "env." + k, Local: nil, Remote: rv, Changed: true})
		case lv != rv:
			fields = append(fields, DiffField{Field: "env." + k, Local: lv, Remote: rv, Changed: true})
		}
	}
	return fields
}

// compareDeps compares local and remote dependencies.
//nolint:unused // Deps comparison pending remote API support
func compareDeps(local, remote map[string]string) []DiffField {
	var fields []DiffField
	if local == nil {
		local = map[string]string{}
	}
	if remote == nil {
		remote = map[string]string{}
	}
	allKeys := make(map[string]bool)
	for k := range local {
		allKeys[k] = true
	}
	for k := range remote {
		allKeys[k] = true
	}
	for k := range allKeys {
		lv, lok := local[k]
		rv, rok := remote[k]
		switch {
		case lok && !rok:
			fields = append(fields, DiffField{Field: "dep." + k, Local: lv, Remote: nil, Changed: true})
		case !lok && rok:
			fields = append(fields, DiffField{Field: "dep." + k, Local: nil, Remote: rv, Changed: true})
		case lv != rv:
			fields = append(fields, DiffField{Field: "dep." + k, Local: lv, Remote: rv, Changed: true})
		}
	}
	return fields
}

func hashLocalSource() (string, error) {
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
		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".ts") ||
			strings.HasSuffix(name, ".py") || strings.HasSuffix(name, ".rs") ||
			strings.HasSuffix(name, ".go") || name == "functionfly.jsonc" || name == "functionfly.json" {
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

func printDiff(result DiffResult) {
	fmt.Printf("\n📋 Diff: %s\n", result.Function)
	fmt.Println(strings.Repeat("─", 60))

	localVer := result.LocalVer
	if localVer == "" {
		localVer = "(none)"
	}
	remoteVer := result.RemoteVer
	if remoteVer == "" {
		remoteVer = "(not deployed)"
	}
	fmt.Printf("  Local version:    %s\n", localVer)
	fmt.Printf("  Remote version:   %s\n", remoteVer)

	if result.SourceHash != "" {
		match := "✅ match"
		if !result.SourceMatch {
			match = "❌ changed"
		}
		fmt.Printf("  Source:           %s\n", match)
	}

	if !result.HasChanges {
		fmt.Printf("\n✅ No changes — local matches deployed state.\n\n")
		return
	}

	fmt.Printf("\n  %-20s  %-20s  %s\n", "FIELD", "LOCAL", "DEPLOYED")
	fmt.Println("  " + strings.Repeat("─", 56))

	for _, c := range result.Changes {
		localStr := formatDiffVal(c.Local)
		remoteStr := formatDiffVal(c.Remote)
		fmt.Printf("  %-20s  %-20s  %s\n", c.Field, localStr, remoteStr)
	}

	fmt.Printf("\n⚡ %d change(s) detected. Run 'ff publish' to deploy.\n\n", len(result.Changes))
}

func formatDiffVal(v interface{}) string {
	if v == nil {
		return "-"
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return "(empty)"
		}
		if len(val) > 18 {
			return val[:15] + "..."
		}
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
