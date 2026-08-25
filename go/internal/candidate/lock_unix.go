//go:build unix

package candidate

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type repositoryLock struct{ file *os.File }

func acquireRepositoryOwnership(commonDir string) (*repositoryLock, error) {
	path := filepath.Join(commonDir, "slopwatch-fix-owner.lock")
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open repository ownership lease: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("repository already has a fix owner: %w", err)
	}
	return &repositoryLock{file: file}, nil
}

func acquireRepositoryLock(commonDir string) (*repositoryLock, error) {
	path := filepath.Join(commonDir, "slopwatch-fix.lock")
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open repository fix lock: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock repository for fix operation: %w", err)
	}
	return &repositoryLock{file: file}, nil
}

func (lock *repositoryLock) Close() error {
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
