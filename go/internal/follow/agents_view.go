package follow

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type agentRenderedRow struct {
	id    AgentRowID
	lines []string
}

func agentsTableView(model Model) string {
	lines := make([]string, 0, model.height)
	lines = append(lines, agentsTopLine(model))
	lines = append(lines, agentsHeader(model))
	lines = append(lines, agentsRows(model)...)
	lines = append(lines, notificationFooter(model, agentsFooterForState(model.agents, model.width)))
	return joinScreenLines(lines)
}

func agentsTopLine(model Model) string {
	logo := "-=[slopwatch]=-"
	left := logo + "  " + fixAggregateText(model.agents.Jobs)
	if model.fixUpdates.stale {
		left += " · UPDATES STALE"
	}
	if model.fixNotice != "" {
		left += " · " + cleanAgentText(model.fixNotice)
	}
	right := cleanAgentText(model.options.Workspace)
	if model.repositoryIdentity != "" {
		right = cleanAgentText(model.repositoryIdentity) + "  " + right
	}
	left = lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render(left)
	return renderTableTitleLine(left, right, model.width)
}

func fixAggregateText(jobs []fix.JobPresentation) string {
	activeAgents := 0
	for _, job := range jobs {
		if job.Phase == fix.PhaseRunning {
			activeAgents += max(1, job.ActorCount)
		}
	}
	return fmt.Sprintf("AGENTS %d", activeAgents)
}

func agentsHeader(model Model) string {
	filter := "ACTIVE"
	if model.agents.ShowAll {
		filter = "ALL"
	}
	tier := responsiveTier(model.width, model.height)
	if crumb := agentBreadcrumb(model.agents, makeAgentLayout(model.width, model.height, bodyHeight(model.mainView, model.height))); crumb != "" {
		prefix := " AGENTS  "
		line := prefix + truncate(crumb, max(0, model.width-lipgloss.Width(prefix)))
		return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).
			Render(padANSI(truncate(line, model.width), model.width))
	}
	line := " AGENTS · " + filter
	if tier == ResponsiveFull && model.width >= 112 {
		line = renderAgentFullColumns(model.width, "", "STATE", "AGENT", "GOAL", "TARGETS", "ACTIVITY", "TIME")
	} else if tier != ResponsiveCompact {
		line = renderAgentMediumColumns(model.width, "", "STATE", "AGENT", "ACTIVITY", "TIME")
	}
	return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).
		Render(padANSI(truncate(line, model.width), model.width))
}

func agentBreadcrumb(state AgentsState, layout agentLayout) string {
	if state.Selected.IsZero() || state.Selected.IsJob() {
		return ""
	}
	rows := state.rows()
	spans := agentRowSpans(rows, layout.Tier)
	selected := agentRowIndex(rows, state.Selected)
	if selected < 0 || spans[selected].start >= state.Offset+layout.Page {
		return ""
	}
	for index := selected - 1; index >= 0; index-- {
		if rows[index].ID.IsJob() && rows[index].ID.JobID == state.Selected.JobID {
			if spans[index].start >= state.Offset {
				return ""
			}
			return strings.Join(nonemptyStrings(
				agentPhaseText(rows[index].Job), cleanAgentText(rows[index].Job.ProfileLabel), cleanAgentText(rows[index].Job.Goal),
			), " · ")
		}
	}
	return ""
}

func agentsRows(model Model) []string {
	bodyHeight := bodyHeight(model.mainView, model.height)
	result := make([]string, 0, bodyHeight)
	rows := model.agents.rows()
	if len(rows) == 0 {
		result = append(result, agentScreenLine(agentEmptyMessage(model.agents), false, model.width, style.TextMuted))
		return appendAgentBlankLines(result, bodyHeight, model.width)
	}

	rendered := make([]agentRenderedRow, 0, len(rows))
	tier := responsiveTier(model.width, model.height)
	policy := agentMetricPolicy{Compact: model.options.Compact, Visible: model.files.Visible, Weights: model.weights, Enabled: model.weightEnabled}
	visibleMetric := func(id fix.MetricID) bool { return policy.visible(id) }
	for _, row := range rows {
		rendered = append(rendered, renderAgentLogicalRow(model.agents.Expanded[row.Job.ID], row, tier, model.width, model.agents.HorizontalOffset, visibleMetric))
	}
	visual := make([]struct {
		id   AgentRowID
		text string
	}, 0, len(rendered)*2)
	for _, row := range rendered {
		for _, line := range row.lines {
			visual = append(visual, struct {
				id   AgentRowID
				text string
			}{row.id, line})
		}
	}
	start := min(max(0, model.agents.Offset), len(visual))
	end := min(len(visual), start+bodyHeight)
	for _, line := range visual[start:end] {
		result = append(result, agentScreenLine(line.text, line.id == model.agents.Selected, model.width, style.TextPrimary))
	}
	return appendAgentBlankLines(result, bodyHeight, model.width)
}

func agentEmptyMessage(state AgentsState) string {
	if strings.TrimSpace(state.FindQuery) != "" {
		return fmt.Sprintf("No fix jobs match %q", cleanAgentText(state.FindQuery))
	}
	if !state.ShowAll {
		for _, job := range state.Jobs {
			if job.Phase == fix.PhaseCompleted {
				return "No active fix jobs - press a to show All"
			}
		}
	}
	return "No fix jobs yet - select a file in Files and press x"
}

func appendAgentBlankLines(lines []string, height, width int) []string {
	for len(lines) < height {
		lines = append(lines, agentScreenLine("", false, width, style.TextPrimary))
	}
	return lines
}

func agentScreenLine(text string, selected bool, width int, foreground lipgloss.Color) string {
	background := style.SelectionSurface(selected)
	return lipgloss.NewStyle().Foreground(foreground).Background(background).
		Render(padANSI(truncate(text, width), width))
}

func renderAgentLogicalRow(expanded bool, row agentLogicalRow, tier ResponsiveTier, width, horizontalOffset int, visibleMetric func(fix.MetricID) bool) agentRenderedRow {
	var lines []string
	if row.File == nil {
		lines = renderAgentJob(row.Job, expanded, tier, width)
	} else {
		lines = renderAgentFile(*row.File, tier, width, horizontalOffset, visibleMetric)
	}
	for index := range lines {
		lines[index] = cleanAgentText(lines[index])
	}
	return agentRenderedRow{id: row.ID, lines: lines}
}

func renderAgentJob(job fix.JobPresentation, expanded bool, tier ResponsiveTier, width int) []string {
	disclosure := "▸"
	if expanded {
		disclosure = "▾"
	}
	state := agentPhaseText(job)
	agent := agentLabel(job, tier)
	goal := cleanAgentText(job.Goal)
	if goal == "" {
		goal = "No goal"
	}
	targets := fmt.Sprintf("%d files", len(job.Targets))
	if len(job.Targets) == 1 {
		targets = "1 file"
	}
	badges := agentBadges(job)
	if tier == ResponsiveCompact {
		first := strings.Join(nonemptyStrings(disclosure, state, agent), "  ")
		second := "  " + strings.Join(nonemptyStrings(agentActivityText(job), targets, badges), " · ")
		return []string{truncate(first, width), truncate(second, width)}
	}
	if tier == ResponsiveMedium || width < 112 {
		activity := agentActivityText(job)
		if activity == "" {
			activity = goal
		}
		return []string{renderAgentMediumColumns(width, disclosure, state, agent, activity, agentJobTime(job, time.Now()))}
	}
	activity := agentActivityText(job)
	if activity == "" {
		activity = "-"
	}
	return []string{renderAgentFullColumns(width, disclosure, state, agent, goal, targets, activity, agentJobTime(job, time.Now()))}
}

func agentActivityText(job fix.JobPresentation) string {
	if job.Phase == fix.PhaseFailed && job.Issue != nil {
		reason := strings.TrimSpace(job.Issue.Detail)
		if reason == "" {
			reason = job.Issue.Summary
		}
		if reason = cleanAgentText(reason); reason != "" {
			return reason
		}
	}
	return cleanAgentText(job.CurrentAction)
}

func renderAgentFullColumns(width int, disclosure, state, agent, goal, targets, activity, elapsed string) string {
	const stateWidth, agentWidth, goalWidth, targetsWidth, timeWidth = 11, 18, 16, 8, 6
	fixed := 3 + stateWidth + agentWidth + goalWidth + targetsWidth + timeWidth + 5
	activityWidth := max(1, width-fixed)
	return agentColumn(disclosure, 3, false) +
		agentColumn(state, stateWidth, false) + " " +
		agentColumn(agent, agentWidth, false) + " " +
		agentColumn(goal, goalWidth, false) + " " +
		agentColumn(targets, targetsWidth, false) + " " +
		agentColumn(activity, activityWidth, false) + " " +
		agentColumn(elapsed, timeWidth, true)
}

func renderAgentMediumColumns(width int, disclosure, state, agent, activity, elapsed string) string {
	const stateWidth, agentWidth, timeWidth = 9, 12, 6
	fixed := 3 + stateWidth + agentWidth + timeWidth + 3
	activityWidth := max(1, width-fixed)
	return agentColumn(disclosure, 3, false) +
		agentColumn(state, stateWidth, false) + " " +
		agentColumn(agent, agentWidth, false) + " " +
		agentColumn(activity, activityWidth, false) + " " +
		agentColumn(elapsed, timeWidth, true)
}

func agentColumn(value string, width int, right bool) string {
	value = truncate(cleanAgentText(value), width)
	padding := strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
	if right {
		return padding + value
	}
	return value + padding
}

func renderAgentFile(file fix.FilePresentation, tier ResponsiveTier, width, horizontalOffset int, visibleMetric func(fix.MetricID) bool) []string {
	classification := agentFileClass(file)
	path := agentFileDisplayPath(file)
	metrics := visibleAgentFileMetrics(file, visibleMetric)
	if tier == ResponsiveCompact {
		prefix := "    " + classification
		path = agentPathWindow(path, agentFilePathViewport(file, tier, width, metrics), horizontalOffset)
		first := prefix + strings.Repeat(" ", max(1, width-lipgloss.Width(prefix)-lipgloss.Width(path))) + path
		second := "       " + metrics
		return []string{
			truncate(first, width),
			truncate(second, width),
		}
	}
	prefix := "    " + classification + "  "
	available := max(1, width-lipgloss.Width(prefix))
	pathWidth := agentFilePathViewport(file, tier, width, metrics)
	metricWidth := max(1, available-pathWidth-2)
	metrics = truncate(metrics, metricWidth)
	pathWidth = max(1, available-lipgloss.Width(metrics)-2)
	path = agentPathWindow(path, pathWidth, horizontalOffset)
	gap := max(2, available-lipgloss.Width(metrics)-lipgloss.Width(path))
	return []string{truncate(prefix+metrics+strings.Repeat(" ", gap)+path, width)}
}

func agentFileDisplayPath(file fix.FilePresentation) string {
	path := cleanAgentText(file.Path.String())
	if file.PreviousPath != "" {
		path = cleanAgentText(file.PreviousPath.String()) + " → " + path
	}
	return path
}

func agentFilePathViewport(file fix.FilePresentation, tier ResponsiveTier, width int, metrics string) int {
	pathWidth := lipgloss.Width(agentFileDisplayPath(file))
	if tier == ResponsiveCompact {
		prefix := "    " + agentFileClass(file)
		return max(1, width-lipgloss.Width(prefix)-1)
	}
	prefix := "    " + agentFileClass(file) + "  "
	available := max(1, width-lipgloss.Width(prefix))
	minimumPathWidth := min(pathWidth, max(12, available/3))
	metricWidth := max(1, available-minimumPathWidth-2)
	metrics = truncate(metrics, metricWidth)
	return max(1, available-lipgloss.Width(metrics)-2)
}

func agentPathWindow(path string, width, offset int) string {
	width = max(1, width)
	total := lipgloss.Width(path)
	if total <= width {
		return path
	}
	offset = min(max(0, offset), total-width)
	end := total - offset
	return ansi.Cut(path, max(0, end-width), end)
}

func agentFileClass(file fix.FilePresentation) string {
	if file.ScopeViolation {
		return "!"
	}
	switch strings.ToLower(strings.TrimSpace(file.Classification)) {
	case "target", "declared", "t":
		return "T"
	case "supporting", "collateral", "s":
		return "S"
	case "provisional", "?":
		return "?"
	case "violation", "outside", "!":
		return "!"
	default:
		return "T"
	}
}

func agentFileMetrics(file fix.FilePresentation) string {
	return agentFileMetricsMatching(file, func(fix.MetricValue) bool { return true })
}

func visibleAgentFileMetrics(file fix.FilePresentation, visibleMetric func(fix.MetricID) bool) string {
	return agentFileMetricsMatching(file, func(metric fix.MetricValue) bool {
		return metric.Complete && visibleMetric(metric.ID)
	})
}

type agentMetricPolicy struct {
	Compact bool
	Visible map[string]bool
	Weights map[string]float64
	Enabled map[string]bool
}

func (policy agentMetricPolicy) visible(id fix.MetricID) bool {
	if policy.Compact || !policy.Visible[string(id)] {
		return false
	}
	definition, known := scoring.MetricDefinitionByID(scoring.MetricID(id))
	if !known {
		return false
	}
	if definition.ComponentID != "" {
		return scoring.NewPolicy(policy.Weights, policy.Enabled).Enabled(definition.ComponentID)
	}
	for _, item := range componentWeights {
		if item.axis == definition.Axis && scoring.NewPolicy(policy.Weights, policy.Enabled).Enabled(item.id) {
			return true
		}
	}
	return false
}

func agentMetricColumnTitle(id fix.MetricID) string {
	for _, definition := range columnDefinitions {
		if definition.key == string(id) {
			return definition.title
		}
	}
	return strings.ToUpper(string(id))
}

func agentMetricColumnRank(id fix.MetricID) int {
	for index, definition := range columnDefinitions {
		if definition.key == string(id) {
			return index
		}
	}
	return len(columnDefinitions)
}

func agentVerificationGlyph(verification string) string {
	value := strings.ToLower(strings.TrimSpace(verification))
	if strings.Contains(value, "final") || strings.Contains(value, "verified") {
		return "✓"
	}
	if strings.Contains(value, "checkpoint") {
		return "◇"
	}
	return ""
}

func agentPhaseText(job fix.JobPresentation) string {
	if job.Phase == fix.PhaseFailed {
		if job.Issue != nil && strings.EqualFold(job.Issue.Code, "canceled") {
			return "CANCELED"
		}
		return "FAILED"
	}
	switch job.Phase {
	case fix.PhaseQueued:
		return "QUEUED"
	case fix.PhasePreflight:
		return "PREFLIGHT"
	case fix.PhasePreparing:
		return "PREPARING"
	case fix.PhaseRunning:
		return "RUNNING"
	case fix.PhaseWaitingVerifier:
		return "WAITING"
	case fix.PhaseVerifying:
		return "VERIFYING"
	case fix.PhaseFailed:
		return "FAILED"
	case fix.PhasePublishing:
		return "COMMITTING"
	case fix.PhaseReconciling:
		return "SYNCING"
	case fix.PhaseCanceling:
		return "CANCELING"
	case fix.PhaseCanceled:
		return "CANCELED"
	case fix.PhaseCompleted:
		return "DONE"
	case fix.PhaseDiscarded:
		return "DISCARDED"
	default:
		return strings.ToUpper(cleanAgentText(string(job.Phase)))
	}
}

func agentLabel(job fix.JobPresentation, tier ResponsiveTier) string {
	parts := []string{cleanAgentText(job.ProfileLabel)}
	if tier == ResponsiveFull {
		parts = append(parts, cleanAgentText(job.ModelLabel), cleanAgentText(job.EffortLabel))
	} else if tier == ResponsiveCompact {
		parts = append(parts, cleanAgentText(job.EffortLabel))
	}
	parts = nonemptyStrings(parts...)
	if len(parts) == 0 {
		return "Unassigned"
	}
	return strings.Join(parts, " / ")
}

func agentBadges(job fix.JobPresentation) string {
	badges := make([]string, 0, 4)
	if job.ActorCount > 1 {
		badges = append(badges, fmt.Sprintf("team %d", job.ActorCount))
	}
	if job.WarningCount > 0 {
		badges = append(badges, fmt.Sprintf("%d warning", job.WarningCount))
	}
	return strings.Join(badges, " · ")
}

func agentJobTime(job fix.JobPresentation, now time.Time) string {
	if job.CreatedAt.IsZero() {
		return "-"
	}
	end := now
	if agentJobFinished(job.Phase) {
		if !job.FinishedAt.IsZero() {
			end = job.FinishedAt
		} else if !job.UpdatedAt.IsZero() {
			end = job.UpdatedAt
		}
	}
	duration := end.Sub(job.CreatedAt)
	if duration < 0 {
		duration = 0
	}
	seconds := int(math.Round(duration.Seconds()))
	if seconds >= 3600 {
		return fmt.Sprintf("%dh%02dm", seconds/3600, (seconds%3600)/60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func cleanAgentText(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

func nonemptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func agentsFooterForState(state AgentsState, width int) string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	if state.FindEditing {
		return agentFindFooter(state, width)
	}
	leftItems := agentFooterRowItems(state)
	rightItems := agentFooterGlobalItems(state.ShowAll)
	return agentFooterLayout(background, leftItems, rightItems, width)
}

func agentFindFooter(state AgentsState, width int) string {
	input := style.InputField(state.FindInput.View(), max(8, min(24, width/3)))
	return truncateANSI(hintRow(style.SurfaceFooter, hintItem{"Enter", "apply"}, hintItem{"Esc", "cancel"})+" "+input, width)
}

func agentFooterRowItems(state AgentsState) []hintItem {
	items := []hintItem{{"Tab", "files"}}
	selected := state.Selected
	if selected.IsZero() {
		return items
	}
	if selected.IsJob() {
		if job, ok := jobByID(state.Jobs, selected.JobID); ok && containsFixAction(job.AllowedActions, fix.ActionCancel) {
			items = append(items, hintItem{"C", "cancel"})
		}
		return append(items, hintItem{"Enter", "expand"}, hintItem{"i", "inspect"}, hintItem{"d", "diff"}, hintItem{"l", "logs"})
	}
	return append(items, hintItem{"Enter", "inspect"}, hintItem{"v", "view"}, hintItem{"d", "diff"}, hintItem{"l", "logs"}, hintItem{"i", "metrics"})
}

func agentFooterGlobalItems(showAll bool) []hintItem {
	filterLabel := "all"
	if showAll {
		filterLabel = "active"
	}
	return []hintItem{{"r", "rescan"}, {"a", filterLabel}, {"f", "find"}, {"o", "sort"}, {"s", "settings"}, {"h", "help"}, {"q", "quit"}}
}

func agentFooterLayout(background lipgloss.Style, leftItems, rightItems []hintItem, width int) string {
	left := hintRow(style.SurfaceFooter, leftItems...)
	right := hintRow(style.SurfaceFooter, rightItems...)
	for lipgloss.Width(left)+lipgloss.Width(right) > width && len(rightItems) > 1 {
		rightItems = rightItems[:len(rightItems)-1]
		right = hintRow(style.SurfaceFooter, rightItems...)
	}
	for lipgloss.Width(left)+lipgloss.Width(right) > width && len(leftItems) > 1 {
		leftItems = leftItems[:len(leftItems)-1]
		left = hintRow(style.SurfaceFooter, leftItems...)
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > width {
		right = ""
	}
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(right))
	return truncateANSI(left+background.Render(strings.Repeat(" ", gap))+right, width)
}

func joinScreenLines(lines []string) string { return strings.Join(lines, "\n") }
