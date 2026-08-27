// Package userdata is the single authority for Slopmochi's per-user files.
// Preferences, caches, job state, worktrees, and other durable runtime state
// must all be children of Root.
package userdata

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Root returns the Slopmochi per-user directory without creating it. Linux
// follows the XDG configuration standard; other platforms use ~/.slopmochi so
// application files are kept together rather than spread across OS-specific
// cache and application-support directories.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	return root(runtime.GOOS, os.Getenv("XDG_CONFIG_HOME"), home)
}

func root(goos, xdgConfig, home string) (string, error) {
	if !validAbsolutePath(home) {
		return "", errorsForInvalidHome()
	}
	home = filepath.Clean(home)
	if goos == "linux" {
		base := xdgConfig
		if !validAbsolutePath(base) {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(filepath.Clean(base), "slopmochi"), nil
	}
	return filepath.Join(home, ".slopmochi"), nil
}

func validAbsolutePath(path string) bool {
	return strings.TrimSpace(path) != "" && filepath.IsAbs(path) && !strings.ContainsRune(path, '\x00')
}

func errorsForInvalidHome() error {
	return fmt.Errorf("user home is not an absolute usable path")
}
