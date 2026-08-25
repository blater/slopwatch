package gitmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintCoversContentModeSymlinkAndRenameEndpoints(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.go")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.Entries[0].Path != "new.go" || first.Entries[0].Previous != "old.go" {
		t.Fatalf("rename = %+v", first)
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, _ := Build(root, []byte("R  new.go\x00old.go\x00"))
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content change did not change fingerprint")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	third, _ := Build(root, []byte("R  new.go\x00old.go\x00"))
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("mode change did not change fingerprint")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-one", path); err != nil {
		t.Fatal(err)
	}
	fourth, _ := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-two", path); err != nil {
		t.Fatal(err)
	}
	fifth, _ := Build(root, []byte("R  new.go\x00old.go\x00"))
	if fourth.Fingerprint == fifth.Fingerprint {
		t.Fatal("symlink target change did not change fingerprint")
	}
}

func TestBuildRejectsTraversalAndMalformedStatus(t *testing.T) {
	for _, status := range [][]byte{[]byte("?? ../escape\x00"), []byte("M  file"), []byte("R  new\x00")} {
		if _, err := Build(t.TempDir(), status); err == nil {
			t.Fatalf("accepted %q", status)
		}
	}
}
