package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/slopslap/slopslap/internal/report"
)

func ConfigureTerminalColours() {
	// This interface has a deliberate application palette, including when
	// NO_COLOR is inherited from the caller.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
}

func (model Model) tableView() string {
	lines := make([]string, 0, model.height)
	top := model.options.Workspace
	status := ""
	if model.analyzing {
		status = "SCANNING"
	} else if model.status != "" {
		status = model.status
	}
	if status != "" {
		top += "  " + status
	}
	topText := " " + truncate(top, max(0, model.width-2))
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#9fb4c6")).Background(topBackground).Render(padANSI(topText, model.width)))
	lines = append(lines, model.header())
	files := model.displayFiles()
	page := model.bodyHeight()
	for row := 0; row < page; row++ {
		if model.analyzing && len(files) == 0 && row == page/2 {
			lines = append(lines, model.loadingRow())
			continue
		}
		index := model.offset + row
		if index >= len(files) {
			lines = append(lines, lipgloss.NewStyle().Background(screenBackground).Render(strings.Repeat(" ", model.width)))
			continue
		}
		lines = append(lines, model.renderRow(files[index], index == model.cursor))
	}
	if model.findOpen {
		lines = append(lines, model.findFooter(model.width))
	} else {
		lines = append(lines, model.footer())
	}
	return strings.Join(lines, "\n")
}

func (model Model) findFooter(width int) string {
	background := lipgloss.NewStyle().Background(footerBackground)
	input := model.findInput.View()
	text := " FIND " + input + "  ENTER find  ESC cancel"
	return background.Render(padANSI(truncateANSI(text, width), width))
}

func (model Model) loadingRow() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[model.animationFrame%len(frames)]
	message := frame + "  SCANNING WORKSPACE  " + frame
	left := max(0, (model.width-lipgloss.Width(message))/2)
	line := strings.Repeat(" ", left) + message
	return lipgloss.NewStyle().Foreground(colourGreen).Background(screenBackground).Render(padANSI(truncate(line, model.width), model.width))
}

func (model Model) selectedFile() (report.File, bool) {
	files := model.displayFiles()
	if model.cursor < 0 || model.cursor >= len(files) {
		return report.File{}, false
	}
	return files[model.cursor], true
}

func (model Model) footer() string {
	background := lipgloss.NewStyle().Background(footerBackground)
	result := background.Render(" ")
	for _, item := range [][2]string{{"c", "columns"}, {"s", "sort"}, {"r", "rescan"}, {"v", "view"}, {"f", "find"}, {"n", "next"}, {"h", "help"}, {"q", "quit"}} {
		result += keyHint(item[0], item[1], footerBackground)
		result += background.Render("  ")
	}
	result = truncateANSI(result, model.width)
	if remaining := model.width - lipgloss.Width(result); remaining > 0 {
		result += background.Render(strings.Repeat(" ", remaining))
	}
	return result
}

type column struct {
	key, title string
	width      int
	right      bool
}

func columnNames() []column {
	return []column{{"cog", "COG", 7, false}, {"npath", "NPATH", 8, false}, {"cyclo", "CYCLO", 7, false}, {"deep", "SHALLOW", 4, false}, {"god", "GOD", 6, true}}
}

func headerShift(key string) int {
	switch key {
	case "score":
		return 1
	case "cog":
		return -1
	case "npath":
		return -3
	case "cyclo":
		return -3
	case "deep":
		return -5
	case "god":
		return 3
	default:
		return 0
	}
}

func (model Model) activeColumns() []column {
	if model.options.Compact {
		return nil
	}
	result := []column{}
	for _, column := range columnNames() {
		if model.visible[column.key] {
			result = append(result, column)
		}
	}
	return result
}

func (model Model) header() string {
	columns := []column{{key: "score", title: "SCORE", width: 7, right: true}}
	if !model.options.Compact {
		columns = append(columns, model.activeColumns()...)
	}
	heading := ""
	nominalPosition := 0
	for index, column := range columns {
		title := column.title
		separator := ""
		if index > 0 {
			separator = " "
			if columns[index-1].key == "score" {
				separator = "  "
			}
		}
		if model.sortKey == column.key {
			marked := model.sortIndicator() + title
			// SHALLOW is intentionally wider than the old DEEP heading. Keep
			// its sort marker attached to the heading; the column still reserves
			// the old four-character data width.
			if lipgloss.Width(marked) <= column.width || column.key == "deep" {
				title = marked
			} else if index > 0 {
				separator = model.sortIndicator()
			}
		}
		nominalPosition += lipgloss.Width(separator)
		target := nominalPosition + headerShift(column.key)
		if target < lipgloss.Width(heading) {
			target = lipgloss.Width(heading)
		}
		heading += strings.Repeat(" ", max(0, target-lipgloss.Width(heading))) + title
		nominalPosition += column.width
	}
	heading = truncate(heading, model.width)
	if model.sortKey == "filename" && lipgloss.Width(heading) < model.width {
		heading += " " + model.sortIndicator()
	} else if !model.sortColumnVisible() && lipgloss.Width(heading) < model.width {
		heading += " " + model.sortIndicator() + model.sortTitle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#6f8ca2")).Background(headerBackground).Bold(true).Render(padANSI(heading, model.width))
}

func (model Model) sortColumnVisible() bool {
	if model.sortKey == "score" || model.sortKey == "filename" {
		return true
	}
	return model.visible[model.sortKey]
}

func (model Model) sortTitle() string {
	for _, field := range sortFields() {
		if field.key == model.sortKey {
			return strings.ToUpper(field.label)
		}
	}
	return ""
}

func (model Model) sortIndicator() string {
	if model.sortReverse {
		return "▼"
	}
	return "▲"
}

func (model Model) overlay(base, modal string) string {
	return model.overlayAt(base, modal, 0)
}

func (model Model) overlayBelowTitle(base, modal string) string {
	return model.overlayAt(base, modal, 1)
}

func (model Model) overlayAt(base, modal string, minimumTop int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < model.height {
		baseLines = append(baseLines, strings.Repeat(" ", model.width))
	}
	modalLines := strings.Split(modal, "\n")
	modalWidth := lipgloss.Width(modal)
	modalHeight := len(modalLines)
	left := max(0, (model.width-modalWidth)/2)
	top := max(minimumTop, (model.height-modalHeight)/2)
	for index, modalLine := range modalLines {
		row := top + index
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLine := baseLines[row]
		if lipgloss.Width(baseLine) < model.width {
			baseLine = padANSI(baseLine, model.width)
		}
		modalLine = padANSI(modalLine, modalWidth)
		rightEdge := min(model.width, left+modalWidth)
		baseLines[row] = ansi.Cut(baseLine, 0, left) +
			ansi.Cut(modalLine, 0, rightEdge-left) +
			ansi.Cut(baseLine, rightEdge, model.width)
	}
	return strings.Join(baseLines[:model.height], "\n")
}
