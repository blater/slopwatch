package follow

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func (model Model) renderRow(file report.File, selected bool) string {
	state := model.rows[file.Path]
	background := rowBackground(state, selected, model.options.TrendWindow)
	prefix := model.renderFixedColumns(file, state, background)
	pathWidth := max(0, model.width-lipgloss.Width(prefix))
	line := prefix + renderPath(file.Path, pathWidth, model.pathOffset, background)
	if remaining := model.width - lipgloss.Width(line); remaining > 0 {
		line += lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", remaining))
	}
	return line
}

func (model Model) renderFixedColumns(file report.File, state rowState, background lipgloss.Color) string {
	separator := lipgloss.NewStyle().Background(background).Render(" ")
	marker, markerColour := model.rowMarker(file, state, time.Now())
	scoreWidth := max(1, 7-lipgloss.Width(marker))
	score := styleCell(pad(decimalWithin(file.Score, scoreWidth), scoreWidth, true), scoreColour(file.Score), background)
	if marker != "" {
		score += styleCell(marker, markerColour, background)
	}
	parts := []string{score}
	activeColumns := model.activeColumns()
	for _, column := range activeColumns {
		parts = append(parts, renderMetricCell(file, column, background))
	}
	if len(activeColumns) > 0 {
		parts[0] += separator
		if activeColumns[len(activeColumns)-1].key == "god" {
			parts[len(parts)-1] += separator
		}
	}
	return strings.Join(parts, separator) + separator
}

func (model Model) pathViewportWidth() int {
	prefix := model.renderFixedColumns(report.File{}, rowState{}, style.SurfaceScreen)
	return max(0, model.width-lipgloss.Width(prefix))
}

func (model Model) rowMarker(file report.File, state rowState, now time.Time) (string, lipgloss.Color) {
	switch file.Freshness {
	case report.FreshnessProvisional, report.FreshnessVerifying:
		return "◌", style.AccentWarning
	case report.FreshnessRefreshing:
		return "↻", style.AccentInfo
	case report.FreshnessStaleError:
		return "!", style.AccentCritical
	}
	if marker, colour, ok := newFileMarker(file.Rank, len(model.document.Files), state, now); ok {
		return marker, colour
	}
	if state.scoreChangedAt.IsZero() || now.Sub(state.scoreChangedAt) > model.options.TrendWindow {
		return "", style.TextMuted
	}
	arrow := movementArrow(state.movementDelta)
	if arrow == "" {
		return "", style.TextMuted
	}
	return arrow, directionColour(state.movementDelta)
}

func rowBackground(state rowState, selected bool, window time.Duration) lipgloss.Color {
	if selected {
		return style.SelectionSurface(true)
	}
	if state.editedAt.IsZero() {
		return style.SelectionSurface(false)
	}
	if edited := editBackground(state, time.Now(), window); edited != "" {
		return edited
	}
	return style.SelectionSurface(false)
}

func movementArrow(delta int) string {
	if delta == 0 {
		return ""
	}
	if delta >= 5 {
		return "⇈"
	}
	if delta <= -5 {
		return "⇊"
	}
	if delta > 0 {
		return "↑"
	}
	return "↓"
}

func renderMetricCell(file report.File, column column, background lipgloss.Color) string {
	value, exists, _ := metric(file, column.key)
	text := "-"
	if exists {
		text = metricText(column.key, value)
	}
	colour := metricColour(column.key, value, exists)
	return styleCell(pad(text, column.width, column.right), colour, background)
}

func metricText(key string, value float64) string {
	return roundedIntegerText(value)
}
