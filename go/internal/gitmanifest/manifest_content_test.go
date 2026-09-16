package gitmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintCoversContentAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.go")
	writeManifestContent(t, path, "one", 0o644)
	first := buildRenamedManifest(t, root)
	assertRenamedManifest(t, first)
	writeManifestContent(t, path, "two", 0o644)
	second := buildRenamedManifest(t, root)
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content change did not change fingerprint")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	third := buildRenamedManifest(t, root)
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("mode change did not change fingerprint")
	}
}

func writeManifestContent(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func buildRenamedManifest(t *testing.T, root string) Manifest {
	t.Helper()
	manifest, err := Build(root, []byte("R  new.go\x00old.go\x00"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertRenamedManifest(t *testing.T, manifest Manifest) {
	t.Helper()
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "new.go" || manifest.Entries[0].Previous != "old.go" {
		t.Fatalf("rename = %+v", manifest)
	}
}
