//go:build unix

package openairesponses

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootedRenameCannotBeRedirectedByParentSymlinkSwap(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	candidate := filepath.Join(root, "candidate")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{staging, candidate, outside, filepath.Join(candidate, "parent")} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "temporary"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(staging)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	candidateRoot, err := os.OpenRoot(candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer candidateRoot.Close()
	destination, err := candidateRoot.Open("parent")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	if err := os.Rename(filepath.Join(candidate, "parent"), filepath.Join(candidate, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(candidate, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := renameRootedFile(source, "temporary", destination, "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside target was touched: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(candidate, "moved", "target"))
	if err != nil || string(contents) != "safe" {
		t.Fatalf("pinned destination contents=%q error=%v", contents, err)
	}
}
