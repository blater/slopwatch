package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *DirectService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath, maximum int64) (File, error) {
	if _, err := loadDirectRecord(service.stateRoot, identity); err != nil {
		return File{}, err
	}
	return readCandidateFile(ctx, identity.RepositoryRoot, path, maximum)
}

func readCandidateFile(ctx context.Context, rootPath string, path fix.RepoPath, maximum int64) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if _, err := fix.ParseRepoPath(path.String()); err != nil {
		return File{}, err
	}
	if maximum <= 0 {
		return File{}, errors.New("candidate preview byte limit must be positive")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return File{}, err
	}
	defer root.Close()
	info, err := root.Lstat(path.String())
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("candidate file is not regular")
	}
	opened, err := root.Open(path.String())
	if err != nil {
		return File{}, err
	}
	defer opened.Close()
	readLimit := maximum
	if maximum < int64(^uint64(0)>>1) {
		readLimit++
	}
	contents, err := io.ReadAll(io.LimitReader(opened, readLimit))
	if err != nil {
		return File{}, err
	}
	truncated := info.Size() > maximum || int64(len(contents)) > maximum
	if truncated {
		contents = contents[:maximum]
	}
	hash := sha256.Sum256(contents)
	return File{Path: path, Contents: contents, ContentHash: hex.EncodeToString(hash[:]), Mode: uint32(info.Mode().Perm()), Truncated: truncated}, nil
}
