package delivery

import (
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/blater/slopwatch/internal/isolation"
)

// GitService implements an exact-ref, create-only publication workflow. It
// never force-updates an existing branch and never runs repository hooks or
// signing programs.
type GitService struct {
	executor gitExecutor
}

func NewGitService(runner isolation.Executor) (*GitService, error) {
	if runner == nil {
		return nil, errors.New("Git delivery requires a process runner")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	git, err = filepath.Abs(git)
	if err != nil {
		return nil, err
	}
	return &GitService{executor: gitExecutor{git: git, runner: runner}}, nil
}
