package openairesponses

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (tools *candidateTools) readTargetManifest(ctx context.Context) (readResult, error) {
	if tools.targetManifest == "" {
		return readResult{}, fault("unavailable", "no target manifest was supplied")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	info, err := tools.staging.Lstat(tools.targetManifest)
	if err != nil || !info.Mode().IsRegular() {
		return readResult{}, fault("read_failed", "target manifest is unavailable")
	}
	file, err := tools.staging.Open(tools.targetManifest)
	if err != nil {
		return readResult{}, fault("read_failed", "target manifest could not be opened")
	}
	contents, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return readResult{}, fault("read_failed", "target manifest could not be read")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	return readResult{Path: "target-manifest", Content: string(contents)}, nil
}

func (tools *candidateTools) Close() error {
	return errors.Join(tools.root.Close(), tools.staging.Close())
}

func withinPath(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (tools *candidateTools) revalidateRoot() error {
	resolved, err := filepath.EvalSymlinks(tools.canonicalRoot)
	if err != nil || resolved != tools.canonicalRoot {
		return fault("workspace_changed", "candidate root identity changed")
	}
	pathInfo, err := os.Stat(tools.canonicalRoot)
	if err != nil {
		return fault("workspace_changed", "candidate root is unavailable")
	}
	handleInfo, err := tools.root.Stat(".")
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		return fault("workspace_changed", "candidate root identity changed")
	}
	return nil
}

func parseToolPath(value string, allowRoot bool) (string, error) {
	if allowRoot && (value == "" || value == ".") {
		return ".", nil
	}
	parsed, err := fix.ParseRepoPath(value)
	if err != nil || forbiddenGitPath(value) {
		return "", fault("invalid_path", "path must be a repository-relative path outside .git")
	}
	return parsed.String(), nil
}

func forbiddenGitPath(value string) bool {
	for _, component := range strings.Split(filepath.ToSlash(value), "/") {
		if strings.EqualFold(component, ".git") {
			return true
		}
	}
	return false
}

func (tools *candidateTools) inspect(pathname string, missingFinalAllowed bool) (os.FileInfo, error) {
	if err := tools.revalidateRoot(); err != nil {
		return nil, err
	}
	if pathname == "." {
		return tools.root.Stat(".")
	}
	components := strings.Split(pathname, "/")
	current := ""
	for index, component := range components {
		current = path.Join(current, component)
		info, err := tools.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missingFinalAllowed && index == len(components)-1 {
			return nil, nil
		}
		if err != nil {
			return nil, fault("not_found", "path does not exist")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fault("unsupported_file", "symbolic links are not accessible")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fault("invalid_path", "a path parent is not a directory")
		}
	}
	return tools.root.Lstat(pathname)
}

func (tools *candidateTools) mayWrite(pathname string) error {
	if tools.repository {
		return nil
	}
	repositoryPath, err := fix.ParseRepoPath(pathname)
	if err != nil {
		return fault("invalid_path", "invalid write path")
	}
	if _, allowed := tools.allowed[repositoryPath]; !allowed {
		return fault("write_denied", "path is outside the frozen write scope")
	}
	return nil
}
