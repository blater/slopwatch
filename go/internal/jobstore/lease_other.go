//go:build !unix && !windows

package jobstore

import "errors"

type storeLease struct{}

func acquireStoreLease(string) (*storeLease, error) {
	return nil, errors.New("fix job locking is not supported on this platform")
}

func (*storeLease) Close() error { return nil }
