package gitmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintCoversContentAndMode(t *testing.T) {
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
	second, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content change did not change fingerprint")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	third, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("mode change did not change fingerprint")
	}
}
