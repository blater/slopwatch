package nativeadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/report"
)

type pathMapper struct {
	repositoryRoot string
	analysisRoot   string
	prefix         string
}

func validateWorkspace(repositoryRoot, analysisRoot string) error {
	if repositoryRoot == "" || !filepath.IsAbs(repositoryRoot) {
		return errors.New("repository root must be absolute")
	}
	if analysisRoot != "" && !filepath.IsAbs(analysisRoot) {
		return errors.New("analysis root must be absolute")
	}
	return nil
}

func newPathMapper(repositoryRoot, analysisRoot string) (pathMapper, error) {
	if analysisRoot == "" {
		analysisRoot = repositoryRoot
	}
	repositoryRoot = filepath.Clean(repositoryRoot)
	analysisRoot = filepath.Clean(analysisRoot)
	relative, err := filepath.Rel(repositoryRoot, analysisRoot)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return pathMapper{}, fmt.Errorf("analysis root %q is outside repository root %q", analysisRoot, repositoryRoot)
	}
	prefix := ""
	if relative != "." {
		prefix = filepath.ToSlash(relative)
		if _, err := fix.ParseRepoPath(prefix); err != nil {
			return pathMapper{}, fmt.Errorf("invalid analysis-root prefix: %w", err)
		}
	}
	return pathMapper{repositoryRoot: repositoryRoot, analysisRoot: analysisRoot, prefix: prefix}, nil
}

func (mapper pathMapper) analysisTargets(paths []fix.RepoPath) ([]string, error) {
	result := make([]string, len(paths))
	for index, path := range paths {
		value := path.String()
		if mapper.prefix != "" {
			if value == mapper.prefix || !strings.HasPrefix(value, mapper.prefix+"/") {
				return nil, fmt.Errorf("target %q is outside analysis root %q", path, mapper.prefix)
			}
			value = strings.TrimPrefix(value, mapper.prefix+"/")
		}
		if _, err := fix.ParseRepoPath(value); err != nil {
			return nil, fmt.Errorf("target %q relative to analysis root: %w", path, err)
		}
		result[index] = value
	}
	return result, nil
}

func (mapper pathMapper) repositoryPath(analysisRelative string) (fix.RepoPath, error) {
	path, err := fix.ParseRepoPath(filepath.ToSlash(analysisRelative))
	if err != nil {
		return "", err
	}
	value := path.String()
	if mapper.prefix != "" {
		value = mapper.prefix + "/" + value
	}
	return fix.ParseRepoPath(value)
}

func validateTargets(paths []fix.RepoPath) error {
	if len(paths) == 0 {
		return errors.New("at least one target is required")
	}
	seen := make(map[fix.RepoPath]struct{}, len(paths))
	for _, path := range paths {
		parsed, err := fix.ParseRepoPath(path.String())
		if err != nil || parsed != path {
			return fmt.Errorf("invalid target %q: %w", path, err)
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("duplicate target %q", path)
		}
		seen[path] = struct{}{}
	}
	return nil
}

func exactFiles(document report.Document, mapper pathMapper, targets []fix.RepoPath) (map[fix.RepoPath]report.File, error) {
	wanted := make(map[fix.RepoPath]struct{}, len(targets))
	for _, target := range targets {
		wanted[target] = struct{}{}
	}
	if document.Truncated {
		return nil, errors.New("analysis report is truncated")
	}
	if len(document.Files) != len(targets) {
		return nil, fmt.Errorf("analysis returned %d files for %d exact targets", len(document.Files), len(targets))
	}
	result := make(map[fix.RepoPath]report.File, len(document.Files))
	for _, file := range document.Files {
		path, err := mapper.repositoryPath(file.Path)
		if err != nil {
			return nil, fmt.Errorf("analysis returned invalid path %q: %w", file.Path, err)
		}
		if _, expected := wanted[path]; !expected {
			return nil, fmt.Errorf("analysis returned undeclared target %q", path)
		}
		if _, duplicate := result[path]; duplicate {
			return nil, fmt.Errorf("analysis returned duplicate target %q", path)
		}
		result[path] = file
	}
	for _, target := range targets {
		if _, exists := result[target]; !exists {
			return nil, fmt.Errorf("analysis omitted target %q", target)
		}
	}
	return result, nil
}

type targetFingerprint struct {
	aggregate string
	files     map[fix.RepoPath]string
}

func fingerprintTargets(repositoryRoot string, paths []fix.RepoPath) (targetFingerprint, error) {
	files := make(map[fix.RepoPath]string, len(paths))
	ordered := append([]fix.RepoPath(nil), paths...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	aggregate := sha256.New()
	for _, path := range ordered {
		full := filepath.Join(repositoryRoot, filepath.FromSlash(path.String()))
		info, err := os.Lstat(full)
		if err != nil {
			return targetFingerprint{}, fmt.Errorf("read %q: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return targetFingerprint{}, fmt.Errorf("target %q is not a regular non-symlink file", path)
		}
		file, err := os.Open(full)
		if err != nil {
			return targetFingerprint{}, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return targetFingerprint{}, fmt.Errorf("hash target %q: %w", path, err)
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		files[path] = digest
		aggregate.Write([]byte(path.String()))
		aggregate.Write([]byte{0})
		aggregate.Write([]byte(digest))
		aggregate.Write([]byte{0})
	}
	return targetFingerprint{aggregate: hex.EncodeToString(aggregate.Sum(nil)), files: files}, nil
}
