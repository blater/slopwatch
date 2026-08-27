package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

// DirectService lets the agent work in the user's current files. Its private
// baseline records only what changed during the job; it never requires a clean
// tree and Discard never rolls back user files.
type DirectService struct {
	stateRoot string
}

type directRecord struct {
	Version  int                   `json:"version"`
	Identity fix.CandidateIdentity `json:"identity"`
	Targets  []fix.RepoPath        `json:"targets"`
	Allowed  []fix.RepoPath        `json:"allowed"`
	Scope    string                `json:"scope"`
	Baseline []directEntry         `json:"baseline"`
}

type directEntry struct {
	Path fix.RepoPath `json:"path"`
	Mode uint32       `json:"mode"`
	Hash string       `json:"hash"`
}

const directRecordName = "direct.json"

func NewDirectService(stateRoot string) (*DirectService, error) {
	if stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return nil, errors.New("direct candidate service requires an absolute private state root")
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create direct candidate state: %w", err)
	}
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("protect direct candidate state: %w", err)
	}
	root, err := filepath.EvalSymlinks(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("canonicalize direct candidate state: %w", err)
	}
	return &DirectService{stateRoot: root}, nil
}

func (service *DirectService) Prepare(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, error) {
	if !validJobID(request.Job) || request.Mode != fix.WorkspaceCurrent || len(request.Targets) == 0 {
		return fix.CandidateIdentity{}, errors.New("prepare current files: job and targets are required")
	}
	if existing, found, err := service.DiscoverPrepared(ctx, request); err != nil || found {
		return existing, err
	}
	root, analysis, err := canonicalCurrentRoots(request.Workspace)
	if err != nil {
		return fix.CandidateIdentity{}, err
	}
	identity := fix.CandidateIdentity{Job: request.Job, WorkspaceMode: fix.WorkspaceCurrent, Repository: request.Workspace.Repository,
		RepositoryRoot: root, AnalysisRoot: analysis, GitCommonDir: request.Workspace.GitCommonDir, BaseCommit: request.Workspace.BaseCommit,
		StagingRoot: filepath.Join(service.stateRoot, string(request.Job), "staging")}
	baseline, err := scanDirectFiles(ctx, root)
	if err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("snapshot current files: %w", err)
	}
	record := directRecord{Version: 1, Identity: identity, Targets: sortedRepoPaths(request.Targets), Allowed: sortedRepoPaths(request.AllowedPaths), Scope: request.AllowedScope, Baseline: baseline}
	jobRoot := filepath.Join(service.stateRoot, string(request.Job))
	if err := os.Mkdir(jobRoot, 0o700); err != nil {
		return fix.CandidateIdentity{}, fmt.Errorf("reserve current-file candidate: %w", err)
	}
	if err := os.Mkdir(identity.StagingRoot, 0o700); err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, fmt.Errorf("create current-file staging: %w", err)
	}
	if err := writeDirectRecord(filepath.Join(jobRoot, directRecordName), record); err != nil {
		_ = os.RemoveAll(jobRoot)
		return fix.CandidateIdentity{}, err
	}
	return identity, nil
}

func (service *DirectService) DiscoverPrepared(ctx context.Context, request PrepareRequest) (fix.CandidateIdentity, bool, error) {
	if err := ctx.Err(); err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	path := filepath.Join(service.stateRoot, string(request.Job), directRecordName)
	record, err := readDirectRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return fix.CandidateIdentity{}, false, nil
	}
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	root, analysis, err := canonicalCurrentRoots(request.Workspace)
	if err != nil {
		return fix.CandidateIdentity{}, false, err
	}
	if record.Version != 1 || record.Identity.Job != request.Job || record.Identity.WorkspaceMode != fix.WorkspaceCurrent ||
		record.Identity.RepositoryRoot != root || record.Identity.AnalysisRoot != analysis || record.Scope != request.AllowedScope ||
		!sameRepoPaths(record.Targets, sortedRepoPaths(request.Targets)) || !sameRepoPaths(record.Allowed, sortedRepoPaths(request.AllowedPaths)) {
		return fix.CandidateIdentity{}, false, errors.New("current-file candidate state does not match the requested job")
	}
	return record.Identity, true, nil
}

func (service *DirectService) Diff(ctx context.Context, identity fix.CandidateIdentity) (DiffSnapshot, error) {
	record, err := service.record(identity)
	if err != nil {
		return DiffSnapshot{}, err
	}
	current, err := scanDirectFiles(ctx, identity.RepositoryRoot)
	if err != nil {
		return DiffSnapshot{}, err
	}
	before := directEntryMap(record.Baseline)
	after := directEntryMap(current)
	paths := make(map[fix.RepoPath]bool, len(before)+len(after))
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	ordered := make([]fix.RepoPath, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	hash := sha256.New()
	files := make([]DiffFile, 0)
	for _, path := range ordered {
		left, had := before[path]
		right, has := after[path]
		status := ""
		entry := right
		switch {
		case !had && has:
			status = "A"
		case had && !has:
			status, entry = "D", left
		case left.Hash != right.Hash || left.Mode != right.Mode:
			status = "M"
		}
		if status == "" {
			continue
		}
		_, _ = io.WriteString(hash, path.String()+"\x00"+status+"\x00"+entry.Hash+"\x00")
		files = append(files, DiffFile{Path: path, Status: status, Mode: entry.Mode, Kind: "file", DiffHash: entry.Hash})
	}
	return DiffSnapshot{Files: files, Fingerprint: hex.EncodeToString(hash.Sum(nil)), Scope: fix.ScopeClean}, nil
}

func (service *DirectService) ReadFile(ctx context.Context, identity fix.CandidateIdentity, path fix.RepoPath, maximum int64) (File, error) {
	if _, err := service.record(identity); err != nil {
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

func (service *DirectService) Recover(_ context.Context, identity fix.CandidateIdentity, targets []fix.RepoPath, scope string, allowed []fix.RepoPath) error {
	record, err := service.record(identity)
	if err != nil {
		return err
	}
	if record.Scope != scope || !sameRepoPaths(record.Targets, sortedRepoPaths(targets)) || !sameRepoPaths(record.Allowed, sortedRepoPaths(allowed)) {
		return errors.New("current-file candidate policy changed since admission")
	}
	return nil
}

func (service *DirectService) ReconcileDiscard(_ context.Context, identity fix.CandidateIdentity) error {
	if !validJobID(identity.Job) || identity.WorkspaceMode != fix.WorkspaceCurrent {
		return errors.New("invalid current-file candidate identity")
	}
	return os.RemoveAll(filepath.Join(service.stateRoot, string(identity.Job)))
}
func (service *DirectService) Discard(ctx context.Context, identity fix.CandidateIdentity) error {
	if _, err := service.record(identity); err != nil {
		return err
	}
	return service.ReconcileDiscard(ctx, identity)
}
func (service *DirectService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	return service.Discard(ctx, identity)
}
func (service *DirectService) Close() error { return nil }

func (service *DirectService) record(identity fix.CandidateIdentity) (directRecord, error) {
	if !validJobID(identity.Job) || identity.WorkspaceMode != fix.WorkspaceCurrent {
		return directRecord{}, errors.New("invalid current-file candidate identity")
	}
	record, err := readDirectRecord(filepath.Join(service.stateRoot, string(identity.Job), directRecordName))
	if err != nil {
		return directRecord{}, fmt.Errorf("read current-file candidate: %w", err)
	}
	if record.Version != 1 || record.Identity != identity {
		return directRecord{}, errors.New("current-file candidate identity does not match its record")
	}
	return record, nil
}

func canonicalCurrentRoots(workspace fix.WorkspaceIdentity) (string, string, error) {
	root, err := filepath.EvalSymlinks(workspace.RepositoryRoot)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize workspace: %w", err)
	}
	analysis, err := filepath.EvalSymlinks(workspace.AnalysisRoot)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize analysis root: %w", err)
	}
	if !within(root, analysis) {
		return "", "", errors.New("analysis root is outside the workspace")
	}
	return root, analysis, nil
}

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
func directEntryMap(values []directEntry) map[fix.RepoPath]directEntry {
	result := make(map[fix.RepoPath]directEntry, len(values))
	for _, value := range values {
		result[value.Path] = value
	}
	return result
}
func sortedRepoPaths(values []fix.RepoPath) []fix.RepoPath {
	result := append([]fix.RepoPath(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func sameRepoPaths(left, right []fix.RepoPath) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
func writeDirectRecord(path string, value directRecord) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".direct-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func readDirectRecord(path string) (directRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return directRecord{}, err
	}
	var value directRecord
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&value); err != nil {
		return directRecord{}, err
	}
	return value, nil
}
