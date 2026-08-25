//go:build !unix

package candidate

import "errors"

type repositoryLock struct{}

func acquireRepositoryOwnership(string) (*repositoryLock, error) {
	return nil, errors.New("repository ownership leases are not implemented on this platform")
}

func acquireRepositoryLock(string) (*repositoryLock, error) {
	return nil, errors.New("concurrent Git worktree locking is not implemented on this platform")
}

func (*repositoryLock) Close() error { return nil }
