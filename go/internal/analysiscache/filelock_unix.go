//go:build unix

package analysiscache

import (
	"errors"
	"os"
	"syscall"
)

func acquireFileLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil && !errors.Is(err, os.ErrClosed) {
			// There is no useful recovery action after the atomic commit. Closing
			// the descriptor also releases the advisory lock.
		}
		_ = file.Close()
	}, nil
}
