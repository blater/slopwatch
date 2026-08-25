//go:build !unix

package candidate

func syncDirectory(string) error { return nil }
