package follow

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
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
	if model.repositoryIdentity != "" {
		top = model.repositoryIdentity + "  " + top
	}
	status := ""
	if !model.analyzing && model.status != "" {
		status = model.status
	}
	freshness := model.freshnessStatus()
	if !model.analyzing && freshness != "" {
		if status != "" {
			status += "  "
		}
		status += freshness
	}
	if status != "" {
		top += "  " + status
	}
	logo := lipgloss.NewStyle().Foreground(style.AccentPositive).Bold(true).Render("-=[slopwatch]=-")
	path := lipgloss.NewStyle().Foreground(style.TextPrimary).Render("  " + top)
	topText := logo + path
	if model.analyzing {
		indicator := model.scanningIndicator(freshness)
		leftWidth := max(0, model.width-lipgloss.Width(indicator))
		left := truncateANSI(topText, leftWidth)
		topText = padANSI(left, leftWidth) + indicator
	} else {
		topText = truncateANSI(topText, model.width)
	}
	lines = append(lines, lipgloss.NewStyle().Background(style.SurfaceTop).Render(padANSI(topText, model.width)))
	lines = append(lines, model.header())
	files := model.displayFiles()
	page := model.bodyHeight()
	for row := 0; row < page; row++ {
		index := model.offset + row
		if index >= len(files) {
			lines = append(lines, lipgloss.NewStyle().Background(style.SurfaceScreen).Render(strings.Repeat(" ", model.width)))
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

func (model Model) freshnessStatus() string {
	if model.freshnessStatusReady {
		return model.freshnessStatusText
	}
	return freshnessStatusForFiles(model.document.Files)
}

func (model *Model) refreshFreshnessStatus() {
	model.freshnessStatusText = freshnessStatusForFiles(model.document.Files)
	model.freshnessStatusReady = true
}

func freshnessStatusForFiles(files []report.File) string {
	counts := map[report.Freshness]int{}
	for _, file := range files {
		if file.Freshness != "" && file.Freshness != report.FreshnessCurrent {
			counts[file.Freshness]++
		}
	}
	parts := make([]string, 0, 4)
	for _, item := range []struct {
		freshness report.Freshness
		label     string
	}{
		{report.FreshnessRefreshing, "REFRESHING"},
		{report.FreshnessVerifying, "VERIFYING"},
		{report.FreshnessProvisional, "PROVISIONAL"},
		{report.FreshnessStaleError, "STALE"},
	} {
		if counts[item.freshness] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", item.label, counts[item.freshness]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "CACHE " + strings.Join(parts, " · ")
}

func (model Model) findFooter(width int) string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	input := model.findInput.View()
	text := background.Render(" FIND "+input+"  ") + hintRow(style.SurfaceFooter,
		hintItem{"ENTER", "find"},
		hintItem{"ESC", "cancel"},
	)
	return background.Render(padANSI(truncateANSI(text, width), width))
}

func (model Model) scanningIndicator(message string) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[model.animationFrame%len(frames)]
	if message == "" {
		message = "SCANNING"
	}
	return lipgloss.NewStyle().Foreground(style.AccentPositive).Background(style.SurfaceScreen).Render(frame + message + frame)
}

func (model Model) selectedFile() (report.File, bool) {
	files := model.displayFiles()
	if model.cursor < 0 || model.cursor >= len(files) {
		return report.File{}, false
	}
	return files[model.cursor], true
}

func (model Model) footer() string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	screenItems := [][2]string{{"o", "sort"}, {"r", "rescan"}, {"v", "view"}, {"i", "info"}}
	generalItems := [][2]string{{"f", "find"}, {"n", "next"}, {"s", "settings"}, {"h", "help"}, {"q", "quit"}}
	screenFunctions := footerItems(screenItems)
	generalFunctions := footerItems(generalItems)
	for len(generalItems) > 2 && lipgloss.Width(screenFunctions)+lipgloss.Width(generalFunctions)+1 > model.width {
		generalItems = generalItems[:len(generalItems)-1]
		generalFunctions = footerItems(generalItems)
	}
	for len(screenItems) > 2 && lipgloss.Width(screenFunctions)+lipgloss.Width(generalFunctions)+1 > model.width {
		screenItems = screenItems[:len(screenItems)-1]
		screenFunctions = footerItems(screenItems)
	}
	if lipgloss.Width(screenFunctions)+lipgloss.Width(generalFunctions)+1 > model.width {
		generalItems = nil
		generalFunctions = ""
	}
	result := screenFunctions
	if gap := model.width - lipgloss.Width(screenFunctions) - lipgloss.Width(generalFunctions); gap > 0 {
		result += background.Render(strings.Repeat(" ", gap))
	}
	return truncateANSI(result+generalFunctions, model.width)
}

func footerItems(items [][2]string) string {
	hints := make([]hintItem, 0, len(items))
	for _, item := range items {
		hints = append(hints, hintItem{key: item[0], label: item[1]})
	}
	return hintRow(style.SurfaceFooter, hints...)
}

type column struct {
	key, title, shortDescription string
	width                        int
	right, defaultVisible        bool
	configurable                 bool
	headerShift                  int
}

var columnDefinitions = []column{
	{"score", "SCORE", "overall score", 8, true, true, false, 1},
	{"cog", "COG", "cognitive", 6, false, true, true, -1},
	{"npath", "NPATH", "execution path complexity", 8, false, true, true, -2},
	{"cyclo", "CYCLO", "cyclomatic complexity", 7, false, true, true, -3},
	{"deep", "SHALLOW", "module depth", 4, false, true, true, -4},
	{"god", "GOD", "responsibility concentration", 6, true, true, true, 2},
	{"coupling", "CPL", "dependency entanglement", 5, true, true, true, 1},
	{"nesting", "NEST", "deep nesting", 7, true, false, true, 0},
	{"typesafety", "TYPE", "type safety", 5, true, false, true, 0},
}

func columnNames() []column {
	return columnDefinitions[1:]
}

func defaultColumnVisibility() map[string]bool {
	visible := make(map[string]bool)
	for _, column := range columnNames() {
		if column.defaultVisible {
			visible[column.key] = true
		}
	}
	return visible
}

func headerShift(key string) int {
	for _, column := range columnDefinitions {
		if column.key == key {
			return column.headerShift
		}
	}
	return 0
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
	columns := []column{columnDefinitions[0]}
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
	return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).Render(padANSI(heading, model.width))
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
			return field.title
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
