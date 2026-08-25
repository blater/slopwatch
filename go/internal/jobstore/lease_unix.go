//go:build unix

package jobstore

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type storeLease struct{ file *os.File }

func acquireStoreLease(directory string) (*storeLease, error) {
	path := filepath.Join(directory, ".jobs.lock")
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open job store lease: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure job store lease: %w", err)
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("job store is already owned by another process: %w", err)
	}
	return &storeLease{file: file}, nil
}

func (lease *storeLease) Close() error {
	if lease == nil || lease.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(lease.file.Fd()), unix.LOCK_UN)
	closeErr := lease.file.Close()
	lease.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
