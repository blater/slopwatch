// Package sourcefs provides the observable filesystem boundary for source inventory.
package sourcefs

import "os"

// FileSystem deliberately exposes individual operations so tests can prohibit
// enumeration after startup while counting named-path reads independently.
type FileSystem interface {
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.DirEntry, error)
	ReadFile(string) ([]byte, error)
}
type OS struct{}

func (OS) Stat(path string) (os.FileInfo, error)      { return os.Stat(path) }
func (OS) Lstat(path string) (os.FileInfo, error)     { return os.Lstat(path) }
func (OS) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }
func (OS) ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }
func Default(fs FileSystem) FileSystem {
	if fs == nil {
		return OS{}
	}
	return fs
}
