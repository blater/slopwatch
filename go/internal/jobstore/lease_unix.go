//go:build unix

package jobstore

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type storeLease struct{ file *os.File }

func acquireStoreLease(path string) (*storeLease, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open fix job lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrJobRunning
		}
		return nil, fmt.Errorf("lock fix job: %w", err)
	}
	return &storeLease{file: file}, nil
}

func (lease *storeLease) Close() error {
	if lease == nil || lease.file == nil {
		return nil
	}
	return lease.file.Close()
}
