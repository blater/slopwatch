package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/style"
)

func renderTable(model Model) string {
	lines := make([]string, 0, model.height)
	lines = append(lines, tableTopLines(model)...)
	lines = append(lines, model.header())
	lines = append(lines, tableRows(model)...)
	if model.findOpen {
		lines = append(lines, model.findFooter(model.width))
	} else {
		lines = append(lines, model.footer())
	}
	return strings.Join(lines, "\n")
}

func tableTopLines(model Model) []string {
	topLeft, topRight, bottomLeft, bottomRight := tableTopParts(model)
	return []string{
		renderTableTitleLine(topLeft, topRight, model.width),
		renderTableTitleLine(bottomLeft, bottomRight, model.width),
	}
}

func renderTableTitleLine(left, right string, width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop)
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
	status := tableStatus(model)
	logoStyle := lipgloss.NewStyle().Foreground(style.AccentPositive).Background(style.SurfaceTop).Bold(true)
	const logo = "૮(˶ᵔ ᵕ ᵔ˶)ა"
	topLeft = logoStyle.Render(logo)
	bottomLeft = logoStyle.Width(lipgloss.Width(logo)).Align(lipgloss.Center).Render("botMochi")
	if model.analyzing {
		status = model.scanningIndicator(model.freshnessStatus())
	} else if status != "" {
		status = lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render(status)
	}
	if status != "" {
		topLeft += lipgloss.NewStyle().Background(style.SurfaceTop).Render("  ") + status
	}
	if len(model.agents.Jobs) > 0 {
		topLeft += lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render("  " + fixAggregateText(model.agents.Jobs))
	}
	if model.fixUpdatesStale {
		topLeft += lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render(" · UPDATES STALE")
	}
	return topLeft, model.repositoryIdentity, bottomLeft, model.options.Workspace
}

func tableStatus(model Model) string {
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
	return status
}

func tableRows(model Model) []string {
	files := model.displayFiles()
	lines := make([]string, 0, model.bodyHeight())
	for row := 0; row < model.bodyHeight(); row++ {
		index := model.offset + row
		if index >= len(files) {
			lines = append(lines, lipgloss.NewStyle().Background(style.SurfaceScreen).Render(strings.Repeat(" ", model.width)))
			continue
		}
		lines = append(lines, model.renderRow(files[index], index == model.cursor))
	}
	return lines
}
