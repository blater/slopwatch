package userdata

import (
	"path/filepath"
	"testing"
)

func TestRootKeepsAllUserDataInOnePlatformLocation(t *testing.T) {
	t.Parallel()
	home := filepath.Join(string(filepath.Separator), "users", "ada")
	xdg := filepath.Join(string(filepath.Separator), "config", "ada")
	for _, test := range []struct {
		name, goos, xdg, want string
	}{
		{"linux xdg", "linux", xdg, filepath.Join(xdg, "slopwatch")},
		{"linux standard fallback", "linux", "", filepath.Join(home, ".config", "slopwatch")},
		{"linux ignores relative xdg", "linux", "relative", filepath.Join(home, ".config", "slopwatch")},
		{"darwin", "darwin", xdg, filepath.Join(home, ".slopwatch")},
		{"windows", "windows", xdg, filepath.Join(home, ".slopwatch")},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := root(test.goos, test.xdg, home)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("root() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRootRejectsInvalidHome(t *testing.T) {
	t.Parallel()
	for _, home := range []string{"", "relative", string([]byte{'/', 'x', 0, 'y'})} {
		if _, err := root("darwin", "", home); err == nil {
			t.Fatalf("root accepted invalid home %q", home)
		}
	}
}
