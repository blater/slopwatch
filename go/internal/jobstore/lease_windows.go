//go:build windows

package jobstore

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type storeLease struct {
	file       *os.File
	overlapped windows.Overlapped
}

func acquireStoreLease(path string) (*storeLease, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open fix job lock: %w", err)
	}
	lease := &storeLease{file: file}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lease.overlapped)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrJobRunning
		}
		return nil, fmt.Errorf("lock fix job: %w", err)
	}
	return lease, nil
}

func (lease *storeLease) Close() error {
	if lease == nil || lease.file == nil {
		return nil
	}
	return lease.file.Close()
}
