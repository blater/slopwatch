package follow

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
)

func renderRow(model Model, file report.File, selected bool) string {
	state := model.files.Rows[file.Path]
	now := time.Now()
	background := rowBackgroundAt(state, selected, model.files.Marked[file.Path], model.options.TrendWindow, now)
	if selected {
		normal := rowBackgroundAt(state, false, model.files.Marked[file.Path], model.options.TrendWindow, now)
		background = cursorHighlightBackground(model, background, normal, now)
	}
	prefix := model.files.fileMarkPrefix(file.Path, background) + renderFixedColumns(model, file, state, background)
	pathWidth := max(0, model.width-lipgloss.Width(prefix))
	line := prefix + renderPath(file.Path, pathWidth, model.files.HorizontalOffset, background)
	if remaining := model.width - lipgloss.Width(line); remaining > 0 {
		line += lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", remaining))
	}
	return line
}

func renderFixedColumns(model Model, file report.File, state rowState, background lipgloss.Color) string {
	separator := lipgloss.NewStyle().Background(background).Render(" ")
	marker, markerColour := rowMarker(model, file, state, time.Now())
	fixMarker := fixMarkerForPath(model.agents.Jobs, fix.RepoPath(file.Path))
	scoreWidth := max(1, columnDefinitions[0].width-lipgloss.Width(fixMarker)-lipgloss.Width(marker))
	score := styleCell(fixMarker, style.TextPrimary, background)
	if marker != "" {
		score += styleCell(marker, markerColour, background)
	}
	scoreText := decimalWithin(file.Score, scoreWidth)
	scoreForeground := scoreColour(file.Score)
	if metricPending(file, "score") {
		scoreText, scoreForeground = "…", style.TextMuted
	} else if metricFailed(file, "score") {
		scoreText = "X"
		scoreForeground = style.TextMuted
	}
	score += styleCell(pad(scoreText, scoreWidth, true), scoreForeground, background)
	parts := []string{score}
	activeColumns := activeColumns(model)
	for _, column := range activeColumns {
		parts = append(parts, renderMetricCell(file, column, background))
	}
	if len(activeColumns) > 0 {
		if activeColumns[len(activeColumns)-1].key == "god" {
			parts[len(parts)-1] += separator
		}
	}
	return strings.Join(parts, separator) + separator
}

func (model Model) pathViewportWidth() int {
	prefix := renderFixedColumns(model, report.File{}, rowState{}, style.SurfaceScreen)
	return max(0, model.width-model.files.markColumnWidth()-lipgloss.Width(prefix))
}

func rowMarker(model Model, file report.File, state rowState, now time.Time) (string, lipgloss.Color) {
	switch file.Freshness {
	case report.FreshnessProvisional, report.FreshnessVerifying:
		return "◌", style.AccentWarning
	case report.FreshnessRefreshing:
		return "↻", style.AccentInfo
	case report.FreshnessStaleError:
		return "!", style.AccentCritical
	}
	if fileAnalysisFailed(file) {
		return "!", style.AccentCritical
	}
	if marker, colour, ok := newFileMarker(file.Rank, len(model.files.Document.Files), state, now); ok {
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

func rowBackground(state rowState, selected, marked bool, window time.Duration) lipgloss.Color {
	return rowBackgroundAt(state, selected, marked, window, time.Now())
}

func rowBackgroundAt(state rowState, selected, marked bool, window time.Duration, now time.Time) lipgloss.Color {
	if selected {
		return style.SelectionSurface(true)
	}
	if marked {
		return style.SurfaceMarked
	}
	if state.editedAt.IsZero() {
		return style.SelectionSurface(false)
	}
	if edited := editBackground(state, now, window); edited != "" {
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
	metricState := scoring.Metric(file, column.key).State
	text := "-"
	if metricPending(file, column.key) {
		text = "…"
	} else if metricFailed(file, column.key) {
		text = "X"
	} else if metricState == "not_applicable" {
		text = "N/A"
	}
	if exists {
		text = metricText(column.key, value)
	}
	colour := metricColour(column.key, value, exists)
	return styleCell(pad(text, column.width, column.right), colour, background)
}

func metricText(key string, value float64) string {
	return roundedIntegerText(value)
}
