package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHonorGitignoreDefaultMigrationAndFalseRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.toml")
	if err := os.WriteFile(path, []byte("version = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := LoadOrCreate(path, DefaultDocument())
	if err != nil || !value.Files.HonorGitignore {
		t.Fatalf("old prefs=%+v %v", value.Files, err)
	}
	value.Files.HonorGitignore = false
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrCreate(path, DefaultDocument())
	if err != nil || loaded.Files.HonorGitignore {
		t.Fatalf("false lost: %+v %v", loaded.Files, err)
	}
}

func TestReportPreferencesDoNotWriteMissingOrInvalidFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "prefs.toml")
	if value, err := LoadExisting(path, DefaultDocument()); err != nil || !value.Files.HonorGitignore {
		t.Fatalf("missing prefs: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("report created preference directory")
	}
	if err := os.Chmod(root, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0755)
	if _, err := LoadExisting(filepath.Join(root, "readonly.toml"), DefaultDocument()); err != nil {
		t.Fatal(err)
	}
	os.Chmod(root, 0755)
	path = filepath.Join(root, "invalid.toml")
	os.WriteFile(path, []byte("not toml !!!"), 0600)
	if _, err := LoadExisting(path, DefaultDocument()); err == nil {
		t.Fatal("invalid existing preferences hidden")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not toml !!!" {
		t.Fatal("report modified invalid preference file")
	}
}
