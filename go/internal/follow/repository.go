package follow

import (
	"os"
	"path/filepath"
	"strings"
)

func repositoryIdentity(path string) string {
	root, gitDir, ok := findRepository(path)
	if !ok {
		return ""
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	const branchPrefix = "ref: refs/heads/"
	branch := strings.TrimSpace(string(head))
	if !strings.HasPrefix(branch, branchPrefix) {
		return ""
	}
	branch = strings.TrimPrefix(branch, branchPrefix)
	if branch == "" {
		return ""
	}
	return filepath.Base(root) + ":" + branch
}

func findRepository(path string) (root, gitDir string, ok bool) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	for {
		marker := filepath.Join(current, ".git")
		info, statErr := os.Stat(marker)
		if statErr == nil {
			if info.IsDir() {
				return current, marker, true
			}
			contents, readErr := os.ReadFile(marker)
			if readErr == nil {
				const gitDirPrefix = "gitdir:"
				location := strings.TrimSpace(string(contents))
				if strings.HasPrefix(location, gitDirPrefix) {
					location = strings.TrimSpace(strings.TrimPrefix(location, gitDirPrefix))
					if !filepath.IsAbs(location) {
						location = filepath.Join(current, location)
					}
					return current, filepath.Clean(location), true
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", false
		}
		current = parent
	}
}
