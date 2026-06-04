package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// safeWritePath canonicalises a target write/default path and rejects
// symlinks plus `..` traversal outside the current working directory.
//
// This protects commands that write binaries, manifests, or compiled
// artifacts from being tricked into writing outside the project tree.
func safeWritePath(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("path is empty")
	}
	info, err := os.Lstat(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to write through symlink: %s", target)
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
		return "", fmt.Errorf("refusing to write outside project tree: %s", cleaned)
	}
	return cleaned, nil
}
