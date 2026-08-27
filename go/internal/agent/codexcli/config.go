// Package codexcli adapts the Codex App Server protocol to the
// provider-neutral agent contract.
package codexcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/blater/slopmochi/internal/agent"
)

const RuntimeKind agent.RuntimeKind = "codex-cli"

func resolveExecutable(value string) (string, error) {
	if value == "" {
		value = "codex"
	}
	if !strings.ContainsRune(value, filepath.Separator) {
		resolved, err := exec.LookPath(value)
		if err != nil {
			return "", err
		}
		value = resolved
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("codex executable is not executable: %s", absolute)
	}
	return absolute, nil
}

func canonicalAbsolute(value string) (string, error) {
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("absolute path required")
	}
	cleaned := filepath.Clean(value)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}
	return resolved, nil
}
