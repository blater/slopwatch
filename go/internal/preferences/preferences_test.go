package preferences

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDefaults() Document {
	return Document{
		Version:     CurrentVersion,
		Appearance:  Appearance{Theme: "dark"},
		Table:       Table{VisibleColumns: []string{"cog", "npath"}, SortBy: "score", SortDescending: true},
		Interaction: Interaction{TrendWindow: "10m"},
		Scoring: Scoring{
			WeightStep: 0.5, MaximumWeight: 20,
			Components: map[string]ComponentPreference{
				"cognitive_complexity": {Enabled: true, Weight: 10},
			},
		},
	}
}

func TestLoadOrCreateWritesCompleteDefaultsAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "preferences.toml")
	want := testDefaults()
	got, err := LoadOrCreate(path, want)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scoring.Components["cognitive_complexity"].Weight != 10 {
		t.Fatalf("created preferences = %#v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, fragment := range []string{"version = 1", "[appearance]", "[table]", "[interaction]", "[scoring]", "[scoring.components.cognitive_complexity]"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("preferences file is missing %q:\n%s", fragment, text)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Replace(text, "theme = 'dark'", "theme = 'light'", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadOrCreate(path, want)
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance.Theme != "light" {
		t.Fatalf("theme = %q, want light", got.Appearance.Theme)
	}
}

func TestLoadOrCreateRejectsMalformedUnknownAndFuturePreferences(t *testing.T) {
	for name, contents := range map[string]string{
		"malformed": "version = [",
		"unknown":   "version = 1\nsurprise = true\n",
		"future":    "version = 99\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "preferences.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadOrCreate(path, testDefaults()); err == nil {
				t.Fatal("invalid preferences were accepted")
			}
		})
	}
}

func TestLoadOrCreateOverlaysPartialFileOnCurrentDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	if err := os.WriteFile(path, []byte("version = 1\n[appearance]\ntheme = 'light'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOrCreate(path, testDefaults())
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance.Theme != "light" || got.Table.SortBy != "score" || got.Scoring.Components["cognitive_complexity"].Weight != 10 {
		t.Fatalf("partial overlay lost defaults: %#v", got)
	}
}

func TestSaveReplacesFileWithoutLeavingTemporaryFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "preferences.toml")
	value := testDefaults()
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	value.Appearance.Theme = "light"
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "preferences.toml" {
		t.Fatalf("unexpected preference files: %v", entries)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "theme = 'light'") {
		t.Fatalf("replacement did not persist light theme:\n%s", data)
	}
}
