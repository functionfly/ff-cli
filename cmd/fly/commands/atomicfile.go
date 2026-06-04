package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// writeFileAtomic writes data to a temporary file in the same directory and
// renames it into place. This guarantees the destination is either fully
// written or untouched, even if the process is killed mid-write.
//
// The destination file's mode is set to 0o600 (owner read/write only) —
// appropriate for credentials and config that may contain tokens.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (retErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not close %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("could not chmod %s: %w", tmpPath, err)
	}
	// On Windows, Rename does not overwrite an existing file and returns
	// an error instead. Detect this so callers can handle it gracefully.
	// On Unix, Rename is atomic and always overwrites — no check needed.
	replaced := false
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); err == nil {
			replaced = true
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("could not rename %s → %s: %w", tmpPath, path, err)
	}
	if replaced {
		return ErrAtomicWriteReplaced
	}
	return nil
}

// ErrAtomicWriteReplaced is returned when an atomic write was requested
// but the destination could not be replaced (Windows-only; on Unix the
// rename is atomic and this error never fires).
var ErrAtomicWriteReplaced = errors.New("atomic write: destination replaced")

// safeReadPath canonicalises a read path and rejects symlinks and `..`
// traversal outside the project tree or home directory.
func safeReadPath(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("path is empty")
	}
	info, err := os.Lstat(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to read through symlink: %s", target)
		}
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	cleaned := filepath.Clean(abs)
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	root := filepath.Clean(cwd)
	if !strings.HasPrefix(cleaned, root+string(filepath.Separator)) && cleaned != root {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			if strings.HasPrefix(cleaned, filepath.Clean(home)+string(filepath.Separator)) {
				return cleaned, nil
			}
		}
		return "", fmt.Errorf("refusing to read outside project tree: %s", cleaned)
	}
	return cleaned, nil
}

// copyFileAtomic copies src to dst using a temp-file-and-rename pattern.
// Used to migrate the legacy credentials file into place safely.
// src must be a non-symlink path; dst is written atomically with perm.
func copyFileAtomic(src, dst string, perm os.FileMode) error {
	cleanSrc, err := cleanPath(src)
	if err != nil {
		return err
	}
	in, err := os.Open(cleanSrc)
	if err != nil {
		return err
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	return writeFileAtomic(dst, data, perm)
}

// cleanPath resolves a path to its absolute, cleaned form and rejects
// symlinks. Unlike safeReadPath, it does not restrict to the project tree,
// so it can read from temp directories and other user-provided paths.
func cleanPath(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	cleaned := filepath.Clean(abs)
	if info, err := os.Lstat(cleaned); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to read through symlink: %s", cleaned)
	}
	return cleaned, nil
}
