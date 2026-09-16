package candidate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirectService lets the agent work in the user's current files. Its private
// baseline records only what changed during the job; it never requires a clean
// tree and Discard never rolls back user files.
type DirectService struct {
	stateRoot string
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
