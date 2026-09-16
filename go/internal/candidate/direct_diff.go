package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *DirectService) Diff(ctx context.Context, identity fix.CandidateIdentity) (DiffSnapshot, error) {
	record, err := loadDirectRecord(service.stateRoot, identity)
	if err != nil {
		return DiffSnapshot{}, err
	}
	current, err := scanDirectFiles(ctx, identity.RepositoryRoot)
	if err != nil {
		return DiffSnapshot{}, err
	}
	return directSnapshot{before: record.Baseline, after: current}.diff(), nil
}

type directSnapshot struct {
	before []directEntry
	after  []directEntry
}

func (snapshot directSnapshot) diff() DiffSnapshot {
	before := directEntryMap(snapshot.before)
	after := directEntryMap(snapshot.after)
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
	return DiffSnapshot{Files: files, Fingerprint: hex.EncodeToString(hash.Sum(nil)), Scope: fix.ScopeClean}
}
