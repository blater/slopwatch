//go:build !unix

package delivery

import "errors"

type deliveryLock struct{}

func acquireDeliveryLock(string) (*deliveryLock, error) {
	return nil, errors.New("Git delivery locking is unavailable on this platform")
}
func (*deliveryLock) Close() error { return nil }
