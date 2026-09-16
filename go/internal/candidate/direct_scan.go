package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

func scanDirectFiles(ctx context.Context, root string) ([]directEntry, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	entries := make([]directEntry, 0)
	err = filepath.WalkDir(root, func(full string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		if item.IsDir() {
			if rel != "." && ignoredDirectDirectory(filepath.Base(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		path, err := fix.ParseRepoPath(filepath.ToSlash(rel))
		if err != nil {
			return nil
		}
		info, err := opened.Lstat(path.String())
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := opened.Open(path.String())
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		entries = append(entries, directEntry{Path: path, Mode: uint32(info.Mode().Perm()), Hash: hex.EncodeToString(hash.Sum(nil))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func ignoredDirectDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".slopwatch":
		return true
	}
	return false
}
