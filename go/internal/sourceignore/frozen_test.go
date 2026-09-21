package sourceignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrozenRulesApplyToNewDirectoriesWithoutLoadingEdits(t *testing.T) {
	root := t.TempDir()
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "*.skip\n")
	matcher, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if matcher.Ignored("live.go", false) {
		t.Fatal("initial policy")
	}
	matcher.Freeze()
	write(".gitignore", "*.go\n")
	write("new/.gitignore", "*.go\n")
	if matcher.Ignored("new/live.go", false) || !matcher.Ignored("new/file.skip", false) || !matcher.Unchanged() {
		t.Fatal("frozen policy reloaded disk rules")
	}
	current, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Ignored("new/live.go", false) {
		t.Fatal("one-shot rules no longer load")
	}
	write(".gitignore", "")
	if current.Unchanged() {
		t.Fatal("one-shot validation disabled")
	}
}
