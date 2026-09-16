package gitmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintCoversSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.go")
	writeSymlink(t, path, "target-one")
	first := buildRenamedManifest(t, root)
	assertRenamedSymlink(t, first)
	removePath(t, path)
	writeSymlink(t, path, "target-two")
	second := buildRenamedManifest(t, root)
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("symlink target change did not change fingerprint")
	}
}

func writeSymlink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func removePath(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func assertRenamedSymlink(t *testing.T, manifest Manifest) {
	t.Helper()
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "new.go" || manifest.Entries[0].Previous != "old.go" || manifest.Entries[0].Kind != "symlink" {
		t.Fatalf("rename symlink = %+v", manifest)
	}
}
