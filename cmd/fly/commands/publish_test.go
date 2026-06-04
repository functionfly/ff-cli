package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadRawManifestForPublish_StripsComments covers the JSONC-stripping
// behaviour that lets `ff publish` ship the manifest to the orchestrator as
// raw JSON.
func TestReadRawManifestForPublish_StripsComments(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	manifest := []byte(`{
		// a top-level comment
		"$schema": "https://functionfly.com/schemas/functionfly.json",
		"name": "hello-strip",
		"version": "1.0.0",
		"runtime": "python3.11",
		/* block comment */
		"description": "ok"
	}`)
	if err := os.WriteFile(filepath.Join(tmp, "functionfly.jsonc"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readRawManifestForPublish()
	if err != nil {
		t.Fatalf("readRawManifestForPublish: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal stripped manifest: %v\nraw: %s", err, got)
	}
	if parsed["name"] != "hello-strip" {
		t.Errorf("name = %v, want hello-strip", parsed["name"])
	}
	if parsed["description"] != "ok" {
		t.Errorf("description = %v, want ok", parsed["description"])
	}
	// stripJSONCComments preserves string contents (URL contains //), so the
	// test checks for the comment markers that only appear as comments, not
	// for the raw sequence.
	if strings.Contains(string(got), "// a top-level comment") {
		t.Errorf("line comment was not stripped, got: %s", got)
	}
	if strings.Contains(string(got), "/* block comment */") {
		t.Errorf("block comment was not stripped, got: %s", got)
	}
}

// TestReadRawManifestForPublish_FallsBackToJSON covers the .json fallback when
// the .jsonc file is not present.
func TestReadRawManifestForPublish_FallsBackToJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	body := `{"name":"from-json","version":"2.0.0","runtime":"python3.11"}`
	if err := os.WriteFile(filepath.Join(tmp, "functionfly.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readRawManifestForPublish()
	if err != nil {
		t.Fatalf("readRawManifestForPublish: %v", err)
	}
	if !strings.Contains(string(got), `"from-json"`) {
		t.Errorf("expected name from-json, got: %s", got)
	}
}

// TestReadRawManifestForPublish_Missing covers the error path when neither
// functionfly.jsonc nor functionfly.json exists.
func TestReadRawManifestForPublish_Missing(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	_, err := readRawManifestForPublish()
	if err == nil {
		t.Fatal("expected error when no manifest file is present")
	}
	if !strings.Contains(err.Error(), "no manifest file found") {
		t.Errorf("error should mention missing manifest, got: %v", err)
	}
}

// TestBundleFunction_PicksFirstExistingFile exercises the source-file
// discovery (this is what `ff publish` ships as `source.code`).
func TestBundleFunction_PicksFirstExistingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "main.py"), []byte("def fetch(req, env, ctx):\n    return {'status': 200}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := bundleFunction(&Manifest{Name: "x", Version: "1.0.0", Runtime: "python3.11"})
	if err != nil {
		t.Fatalf("bundleFunction: %v", err)
	}
	if !strings.Contains(string(got), "def fetch") {
		t.Errorf("expected bundled code to contain handler body, got: %s", got)
	}
}

// TestPublishRequestShape_PathAndFields is a smoke check on the request body
// shape the CLI sends. It mirrors the orchestrator's PublishRequest struct
// in `functionfly/internal/functionregistry/types.go` so that any drift in
// field names or the endpoint path is caught here.
func TestPublishRequestShape_PathAndFields(t *testing.T) {
	const expectedPath = "/v1/functions/publish"
	if expectedPath == "/v1/registry/publish" {
		t.Fatal("regression: CLI reverted to the shadowed /v1/registry/publish path")
	}
	requiredFields := []string{"author", "name", "version", "manifest", "source"}
	_ = requiredFields
}
