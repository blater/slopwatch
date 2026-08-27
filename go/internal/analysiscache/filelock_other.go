//go:build !unix && !windows

package analysiscache

// The Store's in-process per-workspace mutex remains active on platforms for
// which Slopmochi does not yet provide an operating-system file lock.
func acquireFileLock(string) (func(), error) {
	return func() {}, nil
}
