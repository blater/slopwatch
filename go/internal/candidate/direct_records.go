package candidate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func loadDirectRecord(stateRoot string, identity fix.CandidateIdentity) (directRecord, error) {
	if !validJobID(identity.Job) || identity.WorkspaceMode != fix.WorkspaceCurrent {
		return directRecord{}, errors.New("invalid current-file candidate identity")
	}
	record, err := readDirectRecord(filepath.Join(stateRoot, string(identity.Job), directRecordName))
	if err != nil {
		return directRecord{}, fmt.Errorf("read current-file candidate: %w", err)
	}
	if record.Version != 1 || record.Identity != identity {
		return directRecord{}, errors.New("current-file candidate identity does not match its record")
	}
	return record, nil
}

func (record directRecord) matchesPolicy(targets, allowed []fix.RepoPath, scope string) bool {
	return record.Scope == scope && sameRepoPaths(record.Targets, sortedRepoPaths(targets)) && sameRepoPaths(record.Allowed, sortedRepoPaths(allowed))
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
