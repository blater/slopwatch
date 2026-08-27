package analysiscache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blater/slopmochi/internal/userdata"
)

const envelopeMagic = "slopmochi-analysis-cache"

type envelope struct {
	Magic    string          `json:"magic"`
	Store    int             `json:"store_schema"`
	Kind     string          `json:"kind"`
	Schema   int             `json:"schema"`
	Key      string          `json:"key"`
	Checksum Digest          `json:"checksum"`
	Payload  json.RawMessage `json:"payload"`
}

type currentPointer struct {
	ViewKey    ViewKey `json:"view_key"`
	Generation uint64  `json:"generation"`
	Filename   string  `json:"filename"`
	Digest     Digest  `json:"digest"`
}

// Store owns a private, immutable content store and atomic workspace pointers.
// A Store is safe for concurrent readers and writers in one process.
type Store struct {
	root string
}

var processWorkspaceLocks sync.Map // map[canonical store root + ViewKey]*sync.Mutex

// DefaultRoot returns the analysis directory beneath Slopmochi's single
// per-user data root without creating it.
func DefaultRoot() (string, error) {
	root, err := userdata.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "analysis"), nil
}

// NewDefaultStore opens the default per-user cache.
func NewDefaultStore() (*Store, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return NewStore(root)
}

// NewStore creates a private cache hierarchy at root.
func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("cache root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("canonicalize cache root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create cache root: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve cache root: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("make cache root private: %w", err)
	}
	for _, name := range []string{"sources", "artifacts", "units", "workspaces"} {
		path := filepath.Join(absolute, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("create cache directory %s: %w", name, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, fmt.Errorf("make cache directory %s private: %w", name, err)
		}
	}
	return &Store{root: absolute}, nil
}

// Root reports the canonical cache directory.
func (store *Store) Root() string { return store.root }
