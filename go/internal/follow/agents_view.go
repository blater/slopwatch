package follow

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/blater/slopwatch/internal/fix"
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
	lines = append(lines, agentsFooter(model))
	return joinScreenLines(lines)
}

func agentsTopLine(model Model) string {
	logo := "-=[slopwatch]=-"
	jobs := model.visibleAgentJobs()
	left := logo + "  " + fixAggregateText(jobs)
	if model.fixUpdatesStale {
		left += " · UPDATES STALE"
	}
	if model.fixNotice != "" {
		left += " · " + cleanAgentText(model.fixNotice)
	}
	right := cleanAgentText(model.options.Workspace)
	if model.repositoryIdentity != "" {
		right = cleanAgentText(model.repositoryIdentity) + "  " + right
	}
	usable := max(0, model.width-1)
	left = truncate(left, usable)
	rightWidth := max(0, usable-lipgloss.Width(left)-1)
	right = truncateLeft(right, rightWidth)
	gap := max(0, usable-lipgloss.Width(left)-lipgloss.Width(right))
	plain := left + strings.Repeat(" ", gap) + right
	if model.width > 0 {
		plain += " "
	}
	return lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).
		Render(padANSI(truncate(plain, model.width), model.width))
}

func fixAggregateText(jobs []fix.JobPresentation) string {
	counts := map[string]int{}
	for _, job := range jobs {
		counts[agentPhaseText(job)]++
	}
	parts := []string{fmt.Sprintf("FIX %d", len(jobs))}
	for _, state := range []string{
		"FAILED", "CONFLICT", "CANCELED", "REVIEW", "VERIFYING", "WAITING", "RUNNING", "CANCELING",
		"PUBLISHING", "RECONCILING", "DISCARDING", "PREPARING", "PREFLIGHT", "QUEUED", "DONE", "ARCHIVED",
	} {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	return strings.Join(parts, " · ")
}

func agentsHeader(model Model) string {
	jobs := model.visibleAgentJobs()
	filter := "ACTIVE"
	if model.agents.ShowAll {
		filter = "ALL"
	}
	tier := responsiveTier(model.width, model.height)
	left := " FILES [AGENTS]  STATE       ATTEMPT     AGENT          GOAL                 TARGETS  ACTIVITY  TIME"
	if tier == ResponsiveMedium {
		left = " FILES [AGENTS]  STATE       ATTEMPT     AGENT          GOAL                 TARGETS"
	}
	if tier == ResponsiveCompact {
		left = " FILES [AGENTS]  " + filter
	}
	sortKey := model.agents.SortKey
	if sortKey == "" {
		sortKey = "attention"
	}
	right := fmt.Sprintf("%s · %s · %d JOBS ", filter, strings.ToUpper(sortKey), len(jobs))
	if tier == ResponsiveMedium {
		right = ""
	}
	if tier == ResponsiveCompact {
		right = fmt.Sprintf("%d JOBS ", len(jobs))
	}
	if crumb := model.agentBreadcrumb(); crumb != "" {
		prefix := " FILES [AGENTS]  "
		line := prefix + truncate("↑ "+crumb, max(0, model.width-lipgloss.Width(prefix)))
		return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).
			Render(padANSI(truncate(line, model.width), model.width))
	}
	usable := max(0, model.width-lipgloss.Width(right))
	line := truncate(left, usable)
	line = padANSI(line, usable) + right
	return lipgloss.NewStyle().Foreground(style.TextHeader).Background(style.SurfaceHeader).Bold(true).
		Render(padANSI(truncate(line, model.width), model.width))
}

func (model Model) agentBreadcrumb() string {
	if model.agents.Selected.IsZero() || model.agents.Selected.IsJob() {
		return ""
	}
	rows := model.agentRows()
	spans := model.agentRowSpans(rows)
	selected := model.agentRowIndex(rows, model.agents.Selected)
	if selected < 0 || spans[selected].start >= model.agents.Offset+model.bodyHeight() {
		return ""
	}
	for index := selected - 1; index >= 0; index-- {
		if rows[index].ID.IsJob() && rows[index].ID.JobID == model.agents.Selected.JobID {
			if spans[index].start >= model.agents.Offset {
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
	bodyHeight := model.bodyHeight()
	result := make([]string, 0, bodyHeight)
	rows := model.agentRows()
	if len(rows) == 0 {
		result = append(result, agentScreenLine(model.agentEmptyMessage(), false, model.width, style.TextMuted))
		return appendAgentBlankLines(result, bodyHeight, model.width)
	}

	rendered := make([]agentRenderedRow, 0, len(rows))
	for _, row := range rows {
		rendered = append(rendered, renderAgentLogicalRow(model, row))
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

func (model Model) agentEmptyMessage() string {
	if strings.TrimSpace(model.agents.FindQuery) != "" {
		return fmt.Sprintf("No fix jobs match %q", cleanAgentText(model.agents.FindQuery))
	}
	if !model.agents.ShowAll {
		for _, job := range model.agents.Jobs {
			if job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseArchived {
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

func renderAgentLogicalRow(model Model, row agentLogicalRow) agentRenderedRow {
	tier := responsiveTier(model.width, model.height)
	var lines []string
	if row.File == nil {
		lines = renderAgentJob(row.Job, model.agents.Expanded[row.Job.ID], tier, model.width)
	} else {
		lines = renderAgentFile(*row.File, tier, model.width, model.agents.HorizontalOffset)
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
		first := strings.Join(nonemptyStrings(disclosure, state, agent, goal), "  ")
		second := "  " + strings.Join(nonemptyStrings(targets, badges, cleanAgentText(job.CurrentAction)), " · ")
		return []string{truncate(first, width), truncate(second, width)}
	}
	if tier == ResponsiveMedium {
		return []string{fmt.Sprintf("%s  %-11s %-11s %-14s %-20s %s",
			disclosure, truncate(state, 11), truncate(agentAttemptText(job), 11), truncate(agent, 14), truncate(goal, 20), targets)}
	}
	activity := cleanAgentText(job.CurrentAction)
	if activity == "" {
		activity = "-"
	}
	return []string{fmt.Sprintf("%s  %-11s %-11s %-22s %-20s %-8s %-16s %s",
		disclosure, truncate(state, 11), truncate(agentAttemptText(job), 11), truncate(agent, 22), truncate(goal, 20), truncate(targets, 8), truncate(activity, 16), agentJobTime(job, time.Now()))}
}

func renderAgentFile(file fix.FilePresentation, tier ResponsiveTier, width, horizontalOffset int) []string {
	classification := agentFileClass(file)
	path := cleanAgentText(file.Path.String())
	if file.PreviousPath != "" {
		path = cleanAgentText(file.PreviousPath.String()) + " → " + path
	}
	metrics := agentFileMetrics(file)
	if tier == ResponsiveCompact {
		first := "    " + classification + "  " + path
		second := "       " + metrics
		return []string{
			agentScrolledLine(first, width, horizontalOffset, 4),
			agentScrolledLine(second, width, horizontalOffset, 4),
		}
	}
	line := "    " + classification + "  " + path
	available := width - lipgloss.Width(line) - 2
	if available > 8 {
		line += "  " + truncate(metrics, available)
	}
	return []string{agentScrolledLine(line, width, horizontalOffset, 4)}
}

func agentScrolledLine(line string, width, offset, fixed int) string {
	line = cleanAgentText(line)
	if offset <= 0 || lipgloss.Width(line) <= fixed {
		return truncate(line, width)
	}
	prefix := truncate(line, fixed)
	rest := strings.TrimPrefix(line, prefix)
	return prefix + ansi.Cut(rest, offset, offset+max(0, width-lipgloss.Width(prefix)))
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
	baselineMetrics := file.BaselineMetrics
	if len(baselineMetrics) == 0 {
		baselineMetrics = file.Metrics
	}
	if agentFileClass(file) == "S" && file.VerifiedScore == nil && len(baselineMetrics) == 0 {
		if file.Changed {
			status := strings.TrimSpace(file.ChangeStatus)
			if status == "" {
				status = "modified"
			}
			return "supporting file · " + cleanAgentText(status)
		}
		return "supporting file · -"
	}
	verified := "…"
	if file.VerifiedScore != nil {
		verified = roundedIntegerText(*file.VerifiedScore) + agentVerificationGlyph(file.Verification)
	}
	parts := []string{fmt.Sprintf("SCORE %s→%s", roundedIntegerText(file.BaselineScore), verified)}
	verifiedMetrics := make(map[fix.MetricID]fix.MetricValue, len(file.VerifiedMetrics))
	for _, metric := range file.VerifiedMetrics {
		verifiedMetrics[metric.ID] = metric
	}
	for _, metric := range baselineMetrics {
		label := cleanAgentText(metric.Label)
		if label == "" {
			label = strings.ToUpper(string(metric.ID))
		}
		value := "-"
		if metric.Complete {
			value = roundedIntegerText(metric.Value)
		}
		if after, ok := verifiedMetrics[metric.ID]; ok && after.Complete {
			value += "→" + roundedIntegerText(after.Value)
		}
		parts = append(parts, label+" "+value)
	}
	return strings.Join(parts, " · ")
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
	if job.Phase == fix.PhaseAwaitingAction {
		if job.Issue != nil && strings.EqualFold(job.Issue.Code, "canceled") {
			return "CANCELED"
		}
		if job.Scope == fix.ScopeConflicted || job.ConflictCount > 0 {
			return "CONFLICT"
		}
		return "FAILED"
	}
	switch job.Phase {
	case fix.PhaseAdmitted, fix.PhaseQueued:
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
	case fix.PhaseAwaitingReview:
		return "REVIEW"
	case fix.PhasePublishing:
		return "PUBLISHING"
	case fix.PhaseReconciling:
		return "RECONCILING"
	case fix.PhaseCanceling:
		return "CANCELING"
	case fix.PhaseCompleted:
		return "DONE"
	case fix.PhaseArchived:
		return "ARCHIVED"
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
	if attempt := agentAttemptText(job); attempt != "" {
		badges = append(badges, attempt)
	}
	if job.ActorCount > 1 {
		badges = append(badges, fmt.Sprintf("team %d", job.ActorCount))
	}
	if job.WarningCount > 0 {
		badges = append(badges, fmt.Sprintf("%d warning", job.WarningCount))
	}
	if job.ConflictCount > 0 {
		badges = append(badges, fmt.Sprintf("%d conflict", job.ConflictCount))
	}
	return strings.Join(badges, " · ")
}

func agentAttemptText(job fix.JobPresentation) string {
	if job.AttemptOrdinal <= 0 {
		return ""
	}
	return fmt.Sprintf("attempt %d", job.AttemptOrdinal)
}

func agentJobTime(job fix.JobPresentation, now time.Time) string {
	start := job.CreatedAt
	if (job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseArchived) && !job.FinishedAt.IsZero() {
		start = job.FinishedAt
	}
	if start.IsZero() {
		return "-"
	}
	duration := now.Sub(start)
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

func agentsFooter(model Model) string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	leftItems := []hintItem{{"Tab", "files"}}
	if selected := model.agents.Selected; !selected.IsZero() {
		if selected.IsJob() {
			leftItems = append(leftItems, hintItem{"Space", "actions"}, hintItem{"Enter", "expand"}, hintItem{"i", "monitor"}, hintItem{"d", "diff"}, hintItem{"l", "logs"})
			if job, ok := model.selectedAgentJob(); ok && containsFixAction(job.AllowedActions, fix.ActionCancel) {
				leftItems = append(leftItems, hintItem{"C", "cancel"})
			}
		} else {
			leftItems = append(leftItems, hintItem{"Enter", "monitor"}, hintItem{"v", "view"}, hintItem{"d", "diff"}, hintItem{"i", "metrics"})
		}
	}
	if model.agents.FindEditing {
		return truncateANSI(hintRow(style.SurfaceFooter, hintItem{"Enter", "apply"}, hintItem{"Esc", "cancel"})+" "+model.agentFindInput.View(), model.width)
	}
	filterLabel := "all"
	if model.agents.ShowAll {
		filterLabel = "active"
	}
	rightItems := []hintItem{{"a", filterLabel}, {"f", "find"}, {"o", "sort"}, {"s", "settings"}, {"h", "help"}, {"q", "quit"}}
	left := hintRow(style.SurfaceFooter, leftItems...)
	right := hintRow(style.SurfaceFooter, rightItems...)
	for lipgloss.Width(left)+lipgloss.Width(right) > model.width && len(rightItems) > 1 {
		rightItems = rightItems[:len(rightItems)-1]
		right = hintRow(style.SurfaceFooter, rightItems...)
	}
	for lipgloss.Width(left)+lipgloss.Width(right) > model.width && len(leftItems) > 1 {
		leftItems = leftItems[:len(leftItems)-1]
		left = hintRow(style.SurfaceFooter, leftItems...)
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > model.width {
		right = ""
	}
	gap := max(0, model.width-lipgloss.Width(left)-lipgloss.Width(right))
	return truncateANSI(left+background.Render(strings.Repeat(" ", gap))+right, model.width)
}

func joinScreenLines(lines []string) string { return strings.Join(lines, "\n") }
