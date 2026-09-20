package follow

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/style"
)

const (
	tableLogo     = "૮(˶ᵔᵕᵔ˶)ა"
	tableWordmark = "slopWatch"
)

func renderTable(model Model) string {
	lines := make([]string, 0, model.height)
	lines = append(lines, tableTopLines(model)...)
	lines = append(lines, header(model))
	lines = append(lines, tableRows(model)...)
	if model.source.findOpen {
		lines = append(lines, model.source.findFooter(model.width))
	} else {
		lines = append(lines, footer(model))
	}
	return strings.Join(lines, "\n")
}

func tableTopLines(model Model) []string {
	topLeft, topRight, bottomLeft, bottomRight := tableTopParts(model)
	first := renderTableTitleLine(topLeft, topRight, model.width)
	second := renderTableTitleLine(bottomLeft, bottomRight, model.width)
	return []string{
		first,
		underlineANSI(second),
	}
}

// underlineANSI reapplies the underline after every nested style reset in a
// title line. This keeps the rule continuous through padded cells while the
// existing foreground/background styles remain intact. The closing sequence
// prevents the underline from affecting the table header below it.
func underlineANSI(value string) string {
	const underline = "\x1b[4m"
	var result strings.Builder
	result.Grow(len(value) + len(underline) + len("\x1b[24m"))
	result.WriteString(underline)
	for index := 0; index < len(value); {
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '[' {
			if end := strings.IndexByte(value[index+2:], 'm'); end >= 0 {
				end += index + 2
				result.WriteString(value[index : end+1])
				result.WriteString(underline)
				index = end + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(value[index:])
		if size == 0 {
			break
		}
		result.WriteString(value[index : index+size])
		index += size
	}
	result.WriteString("\x1b[24m")
	return result.String()
}

func renderTableTitleLine(left, right string, width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop)
	usableWidth := max(0, width-1)
	right = truncateLeft(right, usableWidth)
	leftWidth := usableWidth - lipgloss.Width(right)
	if left != "" && right != "" {
		leftWidth--
	}
	left = truncateANSI(left, max(0, leftWidth))
	right = titleStyle.Render(right)
	gap := max(0, usableWidth-lipgloss.Width(left)-lipgloss.Width(right))
	topText := left + titleStyle.Render(strings.Repeat(" ", gap)) + right
	if width > 0 {
		topText += titleStyle.Render(" ")
	}
	if remaining := width - lipgloss.Width(topText); remaining > 0 {
		topText += titleStyle.Render(strings.Repeat(" ", remaining))
	}
	return truncateANSI(topText, width)
}

func tableTopParts(model Model) (topLeft, topRight, bottomLeft, bottomRight string) {
	logoStyle := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop).Bold(true)
	logoText := logoStyle.Render(tableLogo)
	centeredWordmark := lipgloss.NewStyle().Width(lipgloss.Width(tableLogo)).Align(lipgloss.Center).Render(tableWordmark)
	wordmark := logoStyle.Render(centeredWordmark)
	statusBackground := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop)
	statusWidth := 16
	if model.analyzing && len(model.runtime.scanProgress) > 0 {
		statusWidth = min(48, max(20, model.width/3))
	}
	_, rightWidth, graphWidth, _ := headerColumnWidths(model.width, lipgloss.Width(logoText), statusWidth)
	graphTop, graphBottom := renderScoreDistribution(model, graphWidth)
	if graphWidth >= 6 && graphTop == "" {
		// Keep the status column stable while the first cached result is loading.
		graphTop, graphBottom = scoreDistributionBlank(graphWidth)
	}
	if graphWidth >= 6 {
		graphTop = underlineANSI(graphTop)
	}
	statusTop, statusBottom := headerStatusCells(model, statusWidth)
	logoGap := statusBackground.Render(strings.Repeat(" ", 3))
	statusGap := statusBackground.Render(strings.Repeat(" ", 2))
	statusTopCell := renderHeaderStatusCell(statusTop, statusWidth)
	statusBottomCell := renderHeaderStatusCell(statusBottom, statusWidth)
	if graphWidth >= 6 {
		topLeft = logoText + logoGap + graphTop + statusGap + statusTopCell
		bottomLeft = wordmark + logoGap + graphBottom + statusGap + statusBottomCell
	} else {
		topLeft = logoText + logoGap + statusTopCell
		bottomLeft = wordmark + logoGap + statusBottomCell
	}
	return topLeft, responsiveHeaderLabel(model.repositoryIdentity, rightWidth), bottomLeft, responsiveHeaderLabel(model.options.Workspace, rightWidth)
}

func headerColumnWidths(width, logoWidth, statusWidth int) (gap, right, graph, statusCellWidth int) {
	if statusWidth <= 0 {
		statusWidth = 16
	}
	// The chart and the right identity column share the space after the
	// unchanged logo. Keep the two-column status block fixed so its meaning
	// stays stable while the chart grows with the terminal.
	const logoGap = 3
	// The extra cell accounts for renderTableTitleLine's left/right gap.
	remaining := width - logoWidth - logoGap - 2 - statusWidth - 1 - 1
	if remaining >= 26 {
		right = max(20, remaining/2)
		if remaining-right < 6 {
			right = remaining - 6
		}
		return logoGap, max(0, right), max(0, remaining-right), statusWidth
	}
	// At physically narrow widths preserve as much of the right identity as
	// possible; renderTableTitleLine will clip the left side as a last resort.
	return logoGap, min(20, max(0, width-1)), 0, statusWidth
}

func responsiveHeaderLabel(value string, budget int) string {
	if budget <= 0 {
		return ""
	}
	value = truncateLeft(value, budget)
	return strings.Repeat(" ", max(0, budget-lipgloss.Width(value))) + value
}

func headerStatusCells(model Model, width int) (string, string) {
	if model.analyzing && len(model.runtime.scanProgress) > 0 {
		top, bottom := scanStatus(model)
		return truncateStatus(top, width), truncateStatus(bottom, width)
	}
	top := compactCacheStatus(model)
	if !model.analyzing && model.status != "" && !overlayPresent(model.overlays, OverlayRuntimeError) {
		top = truncateStatus(model.status, width)
	}
	bottom := ""
	if model.fixUpdates.stale {
		bottom = "AGENTS STALE"
	} else if hasActiveAgents(model.agents.Jobs) {
		bottom = fixAggregateText(model.agents.Jobs)
	}
	return truncateLeft(top, width), truncateLeft(bottom, width)
}

func renderHeaderStatusCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	cellStyle := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop)
	return cellStyle.Render(padANSI(truncateANSI(value, width), width))
}

func hasActiveAgents(jobs []fix.JobPresentation) bool {
	for _, job := range jobs {
		if job.Phase == fix.PhaseRunning {
			return true
		}
	}
	return false
}

func truncateStatus(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}

func compactCacheStatus(model Model) string {
	message := freshnessStatus(model)
	label, count := compactFreshness(message)
	if label == "" {
		if model.analyzing {
			return scanningFrame(model.animationFrame) + " SCANNING"
		}
		return ""
	}
	prefix := ""
	if model.analyzing {
		prefix = scanningFrame(model.animationFrame) + " "
	}
	return prefix + label + " " + count
}

func compactFreshness(message string) (label, count string) {
	fields := strings.Fields(message)
	if len(fields) < 3 || fields[0] != "CACHE" {
		return "", ""
	}
	priority := []struct{ token, label string }{
		{"STALE", "STALE"}, {"REFRESHING", "REFRESH"}, {"VERIFYING", "VERIFY"}, {"PROVISIONAL", "PROV"},
	}
	for _, candidate := range priority {
		for index, field := range fields {
			if field == candidate.token && index+1 < len(fields) {
				count, err := strconv.Atoi(fields[index+1])
				if err != nil {
					return candidate.label, fields[index+1]
				}
				if candidate.token == "PROVISIONAL" {
					return "CACHED", compactCount(count)
				}
				return candidate.label, compactCount(count)
			}
		}
	}
	return "", ""
}

func compactCount(value int) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%dM", value/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%dK", value/1_000)
	}
	return strconv.Itoa(value)
}

func scanningFrame(animationFrame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[animationFrame%len(frames)]
}

func tableRows(model Model) []string {
	files := model.files.displayFiles(model.options.Limit)
	lines := make([]string, 0, bodyHeight(model.mainView, model.height))
	for row := 0; row < bodyHeight(model.mainView, model.height); row++ {
		index := model.files.Offset + row
		if index >= len(files) {
			lines = append(lines, lipgloss.NewStyle().Background(style.SurfaceScreen).Render(strings.Repeat(" ", model.width)))
			continue
		}
		lines = append(lines, renderRow(model, files[index], index == model.files.Cursor))
	}
	return lines
}
