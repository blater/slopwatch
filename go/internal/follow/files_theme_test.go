package follow

import (
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestExclusionsEditorPaintsThemeTextAndBlankCells(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		ConfigureTheme(style.ThemeDark)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		model := settingsRefreshFixture(t)
		ConfigureTheme(theme)
		model.width, model.height = 80, 24
		openSetting(model, "files")
		model.openFilesExclusions()
		for _, value := range []string{"", "generated/\n\n*.generated.go"} {
			model.runtime.filesExclusions.SetValue(value)
			for _, focused := range []bool{true, false} {
				if focused {
					model.runtime.filesExclusions.Focus()
				} else {
					model.runtime.filesExclusions.Blur()
				}
				editor := model.filesExclusionsView()
				foreground := strings.TrimSuffix(strings.TrimPrefix(sourceForegroundPrefix(style.TextPrimary), "\x1b["), "m")
				background := strings.TrimSuffix(strings.TrimPrefix(sourceBackgroundPrefix(style.SurfaceFieldActive), "\x1b["), "m")
				if !strings.Contains(editor, foreground) || !strings.Contains(editor, background) {
					t.Fatalf("%s focused=%v editor does not use theme colors: %q", theme, focused, editor)
				}
				if value == "" && !strings.Contains(ansi.Strip(editor), "One gitignore pattern per line") {
					t.Fatal("placeholder is missing")
				}
				assertPaintedCells(t, editor, model.runtime.filesExclusions.Width(), model.runtime.filesExclusions.Height())
				assertPaintedCells(t, view(*model), model.width, model.height)
			}
		}
	}
}

func TestScreenPaintsEmptyRowsAndPreservesPanelColors(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		ConfigureTheme(style.ThemeDark)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		panel := lipgloss.NewStyle().Foreground(style.AccentInfo).Background(style.SurfaceModal).Render("panel")
		screen := paintScreen(panel+"\n", 20, 5)
		assertPaintedCells(t, screen, 20, 5)
		if !strings.Contains(screen, panel) {
			t.Fatal("painting the screen replaced panel colors")
		}
		blank := strings.Split(screen, "\n")[4]
		if !strings.Contains(blank, sourceStylePrefix(style.TextPrimary, style.SurfaceScreen)) || ansi.Strip(blank) != strings.Repeat(" ", 20) {
			t.Fatalf("%s blank row does not fill the theme background: %q", theme, blank)
		}
	}
}

func assertPaintedCells(t *testing.T, rendered string, width, height int) {
	t.Helper()
	lines := strings.Split(rendered, "\n")
	if len(lines) != height {
		t.Fatalf("rendered %d rows, want %d", len(lines), height)
	}
	for row, line := range lines {
		cells := sourceBackgroundCells(line)
		if len(cells) != width {
			t.Fatalf("row %d rendered %d cells, want %d", row, len(cells), width)
		}
		for column, painted := range cells {
			if !painted {
				t.Fatalf("cell %d:%d has terminal-default background", row, column)
			}
		}
	}
}
