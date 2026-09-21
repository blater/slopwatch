package preferences

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectPreferencesMissingAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	document, err := LoadProject(root)
	if err != nil || document.Files.Exclude != "" {
		t.Fatalf("missing project: %+v %v", document, err)
	}
	path := filepath.Join(root, ".slopwatch.toml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read created file: %v", err)
	}
	document.Files.Exclude = "generated/\n*.go\n!keep.go\n\\#literal\n"
	if err := SaveProject(root, document); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProject(root)
	if err != nil || got != document {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "[files]") || !strings.Contains(string(data), `"""`) {
		t.Fatalf("not multiline TOML: %s %v", data, err)
	}
}

func TestProjectPreferencesErrorsLeaveFileIntact(t *testing.T) {
	for _, contents := range []string{"[files", "[files]\nexclude = [\"a.go\"]\n", "files = false"} {
		root := t.TempDir()
		path := filepath.Join(root, ".slopwatch.toml")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadProject(root); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("accepted malformed preferences %q", contents)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != contents {
			t.Fatalf("malformed file changed: %q %v", data, err)
		}
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".slopwatch.toml"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(root); err == nil {
		t.Fatal("missing read error")
	}
	if err := SaveProject(root, ProjectDocument{}); err == nil {
		t.Fatal("missing write error")
	}
}
