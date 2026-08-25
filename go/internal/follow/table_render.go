package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/style"
)

func renderTable(model Model) string {
	lines := make([]string, 0, model.height)
	lines = append(lines, tableTopLine(model))
	lines = append(lines, model.header())
	lines = append(lines, tableRows(model)...)
	if model.findOpen {
		lines = append(lines, model.findFooter(model.width))
	} else {
		lines = append(lines, model.footer())
	}
	return strings.Join(lines, "\n")
}

func tableTopLine(model Model) string {
	usableWidth := max(0, model.width-1)
	left, right := tableTopParts(model)
	left = truncateANSI(left, usableWidth)
	rightWidth := max(0, usableWidth-lipgloss.Width(left)-1)
	if rightWidth > 0 {
		right = truncateLeft(right, rightWidth)
	} else {
		right = ""
	}
	right = lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render(right)
	gap := max(0, usableWidth-lipgloss.Width(left)-lipgloss.Width(right))
	topText := left + strings.Repeat(" ", gap) + right
	if model.width > 0 {
		topText += " "
	}
	return lipgloss.NewStyle().Background(style.SurfaceTop).Render(padANSI(topText, model.width))
}

func tableTopParts(model Model) (string, string) {
	right := model.options.Workspace
	if model.repositoryIdentity != "" {
		right = model.repositoryIdentity + "  " + right
	}
	status := tableStatus(model)
	logo := lipgloss.NewStyle().Foreground(style.AccentPositive).Bold(true).Render("-=[slopwatch]=-")
	left := logo
	if model.analyzing {
		status = model.scanningIndicator(model.freshnessStatus())
	} else if status != "" {
		status = lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render(status)
	}
	if status != "" {
		left += "  " + status
	}
	if len(model.agents.Jobs) > 0 {
		left += "  " + fixAggregateText(model.agents.Jobs)
	}
	if model.fixUpdatesStale {
		left += " · UPDATES STALE"
	}
	return left, right
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
