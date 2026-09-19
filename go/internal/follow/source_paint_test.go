package follow

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/blater/slopwatch/internal/style"
)

func TestPaintSourceLineKeepsEveryVisibleCellOnDetailSurface(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previousProfile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		line := "\x1b[38;2;200;100;50mif\x1b[0;31mred\x1b[49m  value\x1b[m\r"
		painted := paintSourceLine(line, 20, style.TextPrimary, style.SurfaceDetailBody)
		if got := lipgloss.Width(ansi.Strip(painted)); got != 20 {
			t.Fatalf("%s painted source width = %d, want 20", theme, got)
		}
		if !strings.Contains(painted, ";48;2") {
			t.Fatalf("%s source line has no explicit detail background: %q", theme, painted)
		}
		if !strings.Contains(painted, "\x1b[38;2;200;100;50mif") {
			t.Fatalf("%s syntax foreground was not preserved: %q", theme, painted)
		}
	}
	ConfigureTheme(style.ThemeDark)
}

func TestSourceSGRResetOrderPreservesExplicitColours(t *testing.T) {
	cases := []struct {
		parameters                       string
		foregroundSet, backgroundSet     bool
		foregroundReset, backgroundReset bool
	}{
		{"48;5;123;0", false, false, true, true},
		{"0;38;2;148;0;0", true, false, false, true},
		{"0;1", false, false, true, true},
		{"49;31", true, false, false, true},
	}
	for _, test := range cases {
		got := parseSourceSGRState(test.parameters)
		if got.foregroundSet != test.foregroundSet || got.backgroundSet != test.backgroundSet || got.foregroundReset != test.foregroundReset || got.backgroundReset != test.backgroundReset {
			t.Errorf("SGR %q = %+v", test.parameters, got)
		}
	}
}

func TestSourcePopupPaintsHighlightedBlankAndEOFRows(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previousProfile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		state := sourceState{path: "example.go", viewport: newSourceViewport(40, 12)}
		source := "package example\r\n\r\nfunc main() {\r\n  return\r\n}\r\n// " + strings.Repeat("x", 60)
		state.viewport.SetContent(highlightSource("example.go", source, theme))
		state.viewport.SetXOffset(8)
		if state.viewport.HorizontalScrollPercent() == 0 {
			t.Fatalf("%s source viewport did not retain horizontal scroll", theme)
		}
		view := state.render(40, 12, "")
		if got := lipgloss.Width(ansi.Strip(view)); got != 40 {
			t.Fatalf("%s source popup width = %d, want 40", theme, got)
		}
		for lineIndex, line := range strings.Split(view, "\n") {
			for cellIndex, painted := range sourceBackgroundCells(line) {
				if !painted {
					t.Fatalf("%s source popup cell %d:%d lacks background", theme, lineIndex, cellIndex)
				}
			}
		}
	}
	ConfigureTheme(style.ThemeDark)
}

func sourceBackgroundCells(value string) []bool {
	background := false
	cells := make([]bool, 0, lipgloss.Width(ansi.Strip(value)))
	for index := 0; index < len(value); {
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '[' {
			if end := strings.IndexByte(value[index+2:], 'm'); end >= 0 {
				end += index + 2
				background = sourceTestBackgroundState(strings.TrimSuffix(strings.TrimPrefix(value[index:end+1], "\x1b["), "m"), background)
				index = end + 1
				continue
			}
		}
		character, size := utf8.DecodeRuneInString(value[index:])
		if size == 0 {
			break
		}
		for width := lipgloss.Width(string(character)); width > 0; width-- {
			cells = append(cells, background)
		}
		index += size
	}
	return cells
}

func sourceTestBackgroundState(parameters string, background bool) bool {
	if parameters == "" {
		return false
	}
	values := strings.Split(parameters, ";")
	for index := 0; index < len(values); index++ {
		code, err := strconv.Atoi(values[index])
		if err != nil {
			continue
		}
		switch {
		case code == 0:
			background = false
		case code == 49:
			background = false
		case code == 48:
			background = true
			index += sourceTestExtendedLength(values, index)
		case code == 38 || code == 58:
			index += sourceTestExtendedLength(values, index)
		case code >= 40 && code <= 47 || code >= 100 && code <= 107:
			background = true
		}
	}
	return background
}

func sourceTestExtendedLength(values []string, index int) int {
	if index+1 >= len(values) {
		return 0
	}
	mode, err := strconv.Atoi(values[index+1])
	if err != nil {
		return 0
	}
	if mode == 5 {
		return 2
	}
	if mode == 2 {
		return 4
	}
	return 0
}
