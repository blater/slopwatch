//go:build unix

package delivery

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type deliveryLock struct{ file *os.File }

func acquireDeliveryLock(commonDir string) (*deliveryLock, error) {
	path := filepath.Join(commonDir, "slopwatch-fix.lock")
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &deliveryLock{file: file}, nil
}

func (lock *deliveryLock) Close() error {
	unlock := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closed := lock.file.Close()
	if unlock != nil {
		return unlock
	}
	return closed
}
