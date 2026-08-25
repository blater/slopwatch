//go:build !unix

package jobstore

import "errors"

type storeLease struct{}

func acquireStoreLease(string) (*storeLease, error) {
	return nil, errors.New("durable exclusive job-store leases are not implemented on this platform")
}
func (*storeLease) Close() error { return nil }
func syncDirectory(string) error { return nil }
