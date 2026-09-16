package gitmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintCoversSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.go")
	if err := os.Symlink("target-one", path); err != nil {
		t.Fatal(err)
	}
	first, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.Entries[0].Path != "new.go" || first.Entries[0].Previous != "old.go" || first.Entries[0].Kind != "symlink" {
		t.Fatalf("rename symlink = %+v", first)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-two", path); err != nil {
		t.Fatal(err)
	}
	second, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("symlink target change did not change fingerprint")
	}
}
