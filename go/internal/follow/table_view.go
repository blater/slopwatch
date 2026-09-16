package follow

import (
	"fmt"
	"strconv"
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
	ConfigureTheme(style.ThemeDark)
}

func ConfigureTheme(theme style.Theme) {
	style.ApplyTheme(theme)
	lipgloss.SetHasDarkBackground(theme != style.ThemeLight)
}

func tableView(model Model) string {
	return renderTable(model)
}

func freshnessStatus(model Model) string {
	if model.files.FreshnessStatusReady {
		return model.files.FreshnessStatusText
	}
	return freshnessStatusForFiles(model.files.Document.Files)
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

func scanningIndicator(animationFrame int, message string) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[animationFrame%len(frames)]
	if message == "" {
		message = "SCANNING"
	}
	return lipgloss.NewStyle().Foreground(style.AccentPositive).Background(style.SurfaceTop).Render(frame + message + frame)
}

func footer(model Model) string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	markLabel := "mark"
	if model.files.Marking {
		markLabel = "done"
	}
	screenItems := [][2]string{{"m", markLabel}, {"M", "clear"}, {"o", "sort"}, {"v", "view"}, {"i", "info"}}
	if model.width >= 36 {
		screenItems = append(screenItems[:2], append([][2]string{{"Tab", "agents"}, {"x", "fix"}}, screenItems[2:]...)...)
	}
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
	result := make([]column, 0, len(columnDefinitions))
	for _, column := range columnDefinitions {
		if column.configurable {
			result = append(result, column)
		}
	}
	return result
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

func activeColumns(model Model) []column {
	if model.options.Compact {
		return nil
	}
	result := []column{}
	for _, column := range columnNames() {
		if model.files.Visible[column.key] {
			result = append(result, column)
		}
	}
	return result
}

func headerColumns(model Model) []column {
	columns := []column{columnDefinitions[0]}
	if !model.options.Compact {
		columns = append(columns, activeColumns(model)...)
	}
	return columns
}

func headerCell(model Model, columns []column, index int) (string, string) {
	column := columns[index]
	title := column.title
	separator := ""
	if index > 0 {
		separator = " "
		if columns[index-1].key == "score" {
			separator = "  "
		}
	}
	if model.files.SortKey == column.key {
		marked := sortIndicator(model) + title
		// SHALLOW is intentionally wider than the old DEEP heading. Keep
		// its sort marker attached to the heading; the column still reserves
		// the old four-character data width.
		if lipgloss.Width(marked) <= column.width || column.key == "deep" {
			title = marked
		} else if index > 0 {
			separator = sortIndicator(model)
		}
	}
	return title, separator
}

func placeHeaderColumn(heading string, nominalPosition int, column column, title, separator string) (string, int) {
	nominalPosition += lipgloss.Width(separator)
	target := nominalPosition + headerShift(column.key)
	if target < lipgloss.Width(heading) {
		target = lipgloss.Width(heading)
	}
	heading += strings.Repeat(" ", max(0, target-lipgloss.Width(heading))) + title
	nominalPosition += column.width
	return heading, nominalPosition
}

func buildHeader(model Model, columns []column) string {
	heading := ""
	nominalPosition := 0
	for index, column := range columns {
		title, separator := headerCell(model, columns, index)
		heading, nominalPosition = placeHeaderColumn(
			heading,
			nominalPosition,
			column,
			title,
			separator,
		)
	}
	return heading
}

func headerSortSuffix(model Model, heading string) string {
	if model.files.SortKey == "filename" {
		return heading + " " + sortIndicator(model)
	}
	if !sortColumnVisible(model) {
		return heading + " " + sortIndicator(model) + sortTitle(model)
	}
	return heading
}

func fitHeader(model Model, heading, fileCount string) (string, string) {
	usableWidth := max(0, model.width-1)
	if lipgloss.Width(fileCount) > usableWidth {
		return "", truncateLeft(fileCount, usableWidth)
	}
	return truncateANSI(heading, max(0, usableWidth-lipgloss.Width(fileCount)-1)), fileCount
}

func header(model Model) string {
	columns := headerColumns(model)
	heading := strings.Repeat(" ", model.files.markColumnWidth()) + headerSortSuffix(model, buildHeader(model, columns))
	fileCount := "FILES: " + formatIntegerWithCommas(len(model.files.Document.Files))
	heading, fileCount = fitHeader(model, heading, fileCount)
	usableWidth := max(0, model.width-1)
	gap := max(0, usableWidth-lipgloss.Width(heading)-lipgloss.Width(fileCount))
	heading += strings.Repeat(" ", gap) + fileCount
	if model.width > 0 {
		heading += " "
	}
	return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).Render(padANSI(heading, model.width))
}

func formatIntegerWithCommas(value int) string {
	digits := strconv.Itoa(value)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}

func sortColumnVisible(model Model) bool {
	if model.files.SortKey == "score" || model.files.SortKey == "filename" {
		return true
	}
	return model.files.Visible[model.files.SortKey]
}

func sortTitle(model Model) string {
	for _, field := range sortFields() {
		if field.key == model.files.SortKey {
			return field.title
		}
	}
	return ""
}

func sortIndicator(model Model) string {
	if model.files.SortReverse {
		return "▼"
	}
	return "▲"
}

func (model Model) overlay(base, modal string) string {
	return overlayAt(model, base, modal, 0)
}

func (model Model) overlayBelowTitle(base, modal string) string {
	return overlayAt(model, base, modal, 1)
}

func overlayAt(model Model, base, modal string, minimumTop int) string {
	return overlaySurface(base, modal, model.width, model.height, minimumTop)
}

func overlaySurface(base, modal string, width, height, minimumTop int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, strings.Repeat(" ", width))
	}
	modalLines := strings.Split(modal, "\n")
	modalWidth := lipgloss.Width(modal)
	modalHeight := len(modalLines)
	left := max(0, (width-modalWidth)/2)
	top := max(minimumTop, (height-modalHeight)/2)
	for index, modalLine := range modalLines {
		row := top + index
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLine := baseLines[row]
		if lipgloss.Width(baseLine) < width {
			baseLine = padANSI(baseLine, width)
		}
		modalLine = padANSI(modalLine, modalWidth)
		rightEdge := min(width, left+modalWidth)
		baseLines[row] = ansi.Cut(baseLine, 0, left) +
			ansi.Cut(modalLine, 0, rightEdge-left) +
			ansi.Cut(baseLine, rightEdge, width)
	}
	return strings.Join(baseLines[:height], "\n")
}
