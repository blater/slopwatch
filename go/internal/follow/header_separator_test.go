package follow

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/style"
)

func TestTableTopBarUnderlinesGraphAndSecondRowWithoutLeaking(t *testing.T) {
	ConfigureTheme(style.ThemeDark)
	model := Model{
		width: 100,
		files: FilesState{ScoreDistribution: scoreDistribution{ready: true}},
	}
	lines := tableTopLines(model)
	_, _, graphWidth, _ := headerColumnWidths(model.width, lipgloss.Width(tableLogo), 16)
	assertTitleDimensions(t, lines, model.width)
	assertGraphUnderline(t, lines[0], model.width, lipgloss.Width(tableLogo)+3, graphWidth)
	assertSecondRowUnderline(t, lines[1], header(model), model.width)
}

func assertTitleDimensions(t *testing.T, lines []string, width int) {
	t.Helper()
	if len(lines) != 2 || lipgloss.Width(ansi.Strip(lines[0])) != width || lipgloss.Width(ansi.Strip(lines[1])) != width {
		t.Fatalf("title dimensions changed: %#v", lines)
	}
}

func assertGraphUnderline(t *testing.T, line string, width, start, graphWidth int) {
	t.Helper()
	cells := ansiUnderlineCells(line)
	if len(cells) != width {
		t.Fatalf("first title row cells = %d, want %d", len(cells), width)
	}
	for index, cell := range cells {
		if cell != (index >= start && index < start+graphWidth) {
			t.Fatalf("first title row cell %d underline=%t", index, cell)
		}
	}
}

func assertSecondRowUnderline(t *testing.T, line, following string, width int) {
	t.Helper()
	cells := ansiUnderlineCells(line)
	if len(cells) != width {
		t.Fatalf("second title row cells = %d, want %d", len(cells), width)
	}
	for index, cell := range cells {
		if !cell {
			t.Fatalf("second title row cell %d lost underline", index)
		}
	}
	for index, cell := range ansiUnderlineCells(line + following)[width:] {
		if cell {
			t.Fatalf("underline state leaked into following row at cell %d", index)
		}
	}
}

func ansiUnderlineCells(value string) []bool {
	underlined := false
	cells := make([]bool, 0, lipgloss.Width(ansi.Strip(value)))
	for index := 0; index < len(value); {
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '[' {
			if end := strings.IndexByte(value[index+2:], 'm'); end >= 0 {
				end += index + 2
				parameters := strings.Split(value[index+2:end], ";")
				for parameterIndex, parameter := range parameters {
					switch parameter {
					case "0", "24":
						underlined = false
					case "4":
						// A bare 4 is underline; skip colour payload values.
						if parameterIndex == 0 || len(parameters) == 1 {
							underlined = true
						}
					}
				}
				index = end + 1
				continue
			}
		}
		character, size := utf8.DecodeRuneInString(value[index:])
		if size == 0 {
			break
		}
		for width := lipgloss.Width(string(character)); width > 0; width-- {
			cells = append(cells, underlined)
		}
		index += size
	}
	return cells
}
