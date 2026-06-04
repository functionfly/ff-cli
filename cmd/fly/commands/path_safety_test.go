package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeWritePathBlocksSymlinks(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target")
	if err := os.Symlink("/etc/passwd", target); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	_, err := safeWritePath(target)
	if err == nil {
		t.Fatal("expected error for symlink path, got nil")
	}
}

func TestSafeWritePathRejectsTraversal(t *testing.T) {
	root := t.TempDir()

	outer := filepath.Join(root, "outer")
	if err := os.MkdirAll(outer, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Chdir(inner)

	_, err := safeWritePath(filepath.Join(root, "outer", "evil.txt"))
	if err == nil {
		t.Fatal("expected error for traversal outside cwd, got nil")
	}
}
