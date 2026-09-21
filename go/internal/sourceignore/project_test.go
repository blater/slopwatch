package sourceignore

import (
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/preferences"
)

func TestProjectExclusionsReuseGitignoreRules(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "global.go\n")
	text := "# comment\n/root.go\n*.gen.go\n!keep.gen.go\nblocked/\n!blocked/keep.go\npartial/*\n!partial/keep/\na/**/z.go\n\\#literal.go\n"
	if err := preferences.SaveProject(root, preferences.ProjectDocument{Files: preferences.ProjectFiles{Exclude: text}}); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{false, true} {
		matcher := mustNew(t, root, disabled)
		for path, want := range map[string]bool{
			"root.go": true, "nested/root.go": false, "a.gen.go": true, "keep.gen.go": false,
			"blocked/keep.go": true, "partial/drop.go": true, "partial/keep/a.go": false,
			"a/x/z.go": true, "#literal.go": true, "global.go": !disabled,
		} {
			if got := matcher.Ignored(path, false); got != want {
				t.Errorf("disabled=%v path=%s ignored=%v want=%v", disabled, path, got, want)
			}
		}
		if !matcher.Ignored("blocked", true) {
			t.Fatal("excluded directory is not pruned")
		}
	}
	// A nested workspace reads its own project file, not its parent's file.
	if mustNew(t, filepath.Join(root, "nested"), true).Ignored("a.gen.go", false) {
		t.Fatal("project preferences inherited from a different workspace")
	}
}

func TestProjectLoadErrorsAndUntrackedSnapshot(t *testing.T) {
	root := t.TempDir()
	matcher := mustNew(t, root, true)
	fingerprint := matcher.Fingerprint()
	put(t, root, ".slopwatch.toml", "[files]\nexclude = 'a.go'\n")
	if !matcher.Unchanged() || matcher.Fingerprint() != fingerprint || matcher.Ignored("a.go", false) {
		t.Fatal("existing matcher tracked project TOML changes")
	}
	if !mustNew(t, root, true).Ignored("a.go", false) {
		t.Fatal("new matcher did not load project text")
	}
	put(t, root, ".slopwatch.toml", "[files]\nexclude = 42\n")
	if _, err := New(root, true); err == nil {
		t.Fatal("constructor swallowed project parse error")
	}
}
