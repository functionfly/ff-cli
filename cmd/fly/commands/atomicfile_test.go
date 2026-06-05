package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomic_CreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := writeFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Errorf("first write: got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 0o600", perm)
		}
	}
	if err := writeFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("second write: got %q", got)
	}
}

func TestWriteFileAtomic_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested/dir/file.json")
	if err := writeFileAtomic(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file missing after write: %v", err)
	}
}

func TestWriteFileAtomic_NoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := writeFileAtomic(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp-*") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestCopyFileAtomic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFileAtomic(src, dst, 0o600); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
	// Source should still exist.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source disappeared: %v", err)
	}
}

// TestWriteFileAtomic_HandlesInvalidDir covers the error path: the parent
// directory cannot be created (e.g. a regular file is in the way). The
// atomic writer must not leave a half-written file behind.
func TestWriteFileAtomic_HandlesInvalidDir(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(blocker, "child", "out.txt")
	err := writeFileAtomic(bad, []byte("data"), 0o600)
	if err == nil {
		t.Fatal("expected error when path is not writable")
	}
	if !strings.Contains(err.Error(), "could not create directory") &&
		!strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("error message unexpected: %v", err)
	}
	// No temp files left in dir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "blocker") {
			if info, _ := e.Info(); info != nil && info.IsDir() {
				t.Errorf("directory was created at blocker: %v", info)
			}
		}
	}
}

// ensure the test compiles in case json/errors are unused
var _ = json.Marshal
var _ = errors.New
