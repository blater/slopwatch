package sourceignore

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRuleMatrix(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "# comment\n/root.go\n*.gen.go\nblocked/\n!blocked/keep.go\npartial/*\n!partial/keep/\na/**/z.go\nends/**\n\\#literal.go\n\\!literal.go\nspace\\ \ntrailing.go   \nfile[[:digit:]].go\n[[:alpha:]].go\n/foo[!x]bar.go\n")
	put(t, root, "nested/.gitignore", "!*.gen.go\n/omit?.go\n")
	matcher := New(root, false)
	for path, want := range map[string]bool{
		"root.go": true, "nested/root.go": false, "a.gen.go": true, "nested/a.gen.go": false,
		"blocked/keep.go": true, "partial/drop.go": true, "partial/keep/retained.go": false,
		"a/z.go": true, "a/x/z.go": true, "ends/x/y.go": true,
		"#literal.go": true, "!literal.go": true, "space ": true, "space": false,
		"trailing.go": true, "nested/omit1.go": true, "omit1.go": false,
		"file1.go": true, "filex.go": false, "a.go": true, "1.go": false,
		"foo/bar.go": false, "fooybar.go": true,
	} {
		if got := matcher.Ignored(filepath.Join(root, path), false); got != want {
			t.Errorf("%q ignored=%v, want %v", path, got, want)
		}
	}
}

func TestAncestorRulesAndRepositoryBoundary(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "outside.go\n")
	put(t, root, "repo/.git", "gitdir: external\n")
	put(t, root, "repo/.gitignore", "inside.go\n")
	put(t, root, "repo/sub/file.go", "")
	matcher := New(filepath.Join(root, "repo/sub"), false)
	if matcher.Ignored("outside.go", false) || !matcher.Ignored("inside.go", false) {
		t.Fatal("ancestor boundary not honored")
	}
	inputs := matcher.AncestorInputs()
	if len(inputs) != 2 {
		t.Fatalf("inputs %v", inputs)
	}
	if New(filepath.Join(root, "repo/sub"), true).Ignored("inside.go", false) {
		t.Fatal("disabled matcher ignored source")
	}
}

func TestFingerprintAndPerScanRuleCache(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "old.go\n")
	first := New(root, false)
	before := first.Fingerprint()
	if !first.Ignored("old.go", false) {
		t.Fatal("missing old rule")
	}
	put(t, root, ".gitignore", "new.go\n")
	if first.Ignored("new.go", false) {
		t.Fatal("scan did not retain its rule snapshot")
	}
	next := New(root, false)
	if next.Fingerprint() == before || !next.Ignored("new.go", false) {
		t.Fatal("new scan did not refresh rules")
	}
}

func TestWildmatchByteAndMalformedClasses(t *testing.T) {
	for _, test := range []struct {
		pattern, path string
		want          bool
	}{
		{"[!]]", "a", true}, {"[!]]", "]", false}, {"foo[/]bar", "foo/bar", false},
		{"foo[/-0]bar", "foo0bar", true}, {"foo[/-0]bar", "foo/bar", false},
		{"a[b", "a[b", false}, {"a\\", "a", false}, {"?", "é", false}, {"??", "é", true},
		{"[!abc]", "é", false}, {"é", "é", true},
	} {
		rules := parse("/root", test.pattern)
		if got := matches(rules, filepath.Join("/root", test.path), false); got != test.want {
			t.Errorf("%q %q = %v", test.pattern, test.path, got)
		}
	}
}

func TestNewlinesSymlinkRulesAndIncidentalReadFailure(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "**/ignored.go\nplain.go\n")
	m := New(root, false)
	for _, path := range []string{"dir\nname/ignored.go", "dir\nname/plain.go"} {
		if !m.Ignored(path, false) {
			t.Errorf("newline path not matched: %q", path)
		}
	}
	linkRoot := t.TempDir()
	target := filepath.Join(root, ".gitignore")
	if err := os.Symlink(target, filepath.Join(linkRoot, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if New(linkRoot, false).Ignored("plain.go", false) {
		t.Fatal("symlink rules were followed")
	}
	bad := t.TempDir()
	if err := os.Mkdir(filepath.Join(bad, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	m = New(bad, false)
	m.Ignored("plain.go", false)
	warnings := m.Diagnostics()
	if len(warnings) != 1 || warnings[0]["severity"] != "info" || warnings[0]["attributes"].(map[string]any)["log_only"] != true {
		t.Fatalf("missing incidental read evidence: %v", warnings)
	}
}

func TestFingerprintUsesCapturedRulesWithoutWalkingUnvisitedTree(t *testing.T) {
	root := t.TempDir()
	put(t, root, "visited/.gitignore", "old.go\n")
	put(t, root, "unvisited/.gitignore", "one.go\n")
	m := New(root, false)
	m.Ignored("visited/old.go", false)
	before := m.Fingerprint()
	count := len(m.inputs)
	for i := 0; i < 100; i++ {
		if m.Fingerprint() != before || len(m.inputs) != count {
			t.Fatal("fingerprinting revisited inventory")
		}
	}
	put(t, root, "unvisited/.gitignore", "two.go\n")
	if !m.Unchanged() {
		t.Fatal("unvisited rule entered snapshot")
	}
	put(t, root, "visited/.gitignore", "new.go\n")
	if m.Unchanged() || m.Fingerprint() != before {
		t.Fatal("snapshot failed mutation validation or changed captured identity")
	}
}

func TestSnapshotIncludesMissingAndLogicalSymlinkRules(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	put(t, target, ".gitignore", "first.go\n")
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	m := New(root, false)
	if !m.Ignored("link/first.go", false) {
		t.Fatal("logical rules absent")
	}
	first := m.Fingerprint()
	put(t, target, ".gitignore", "second.go\n")
	next := New(root, false)
	next.Ignored("link/first.go", false)
	if m.Unchanged() || next.Fingerprint() == first {
		t.Fatal("authorized symlink rules absent from snapshot identity")
	}
	put(t, root, "empty/file.go", "")
	m = New(root, false)
	m.Ignored("empty/file.go", false)
	put(t, root, "empty/.gitignore", "*.go\n")
	if m.Unchanged() {
		t.Fatal("new missing rule file not detected")
	}
}
