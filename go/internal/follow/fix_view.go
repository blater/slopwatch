package follow

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (model Model) featureOverlayView(base string, frame OverlayFrame) string {
	switch frame.Kind {
	case OverlayFixForm:
		return model.overlay(base, fixDialogPopup(model.fixDialog, model.profileCatalog, model.width, model.height))
	case OverlayTargetScoreEditor:
		return fixTargetScoreEditorView(base, model.fixDialog, model.profileCatalog, model.width, model.height)
	case OverlayConfigSettings:
		if fullScreenSurface(model.width, model.height) {
			return configSettingsFullScreen(model.configSettings, model.profileCatalog, model.width, model.height)
		}
		return model.overlay(base, configSettingsPopup(model.configSettings, model.profileCatalog, model.width, model.height))
	case OverlayPromptEditor:
		return masterPromptEditorView(model.configSettings, model.width, model.height)
	case OverlayJobMonitor:
		return jobMonitorView(base, model.jobMonitor, model.width, model.height, model.agentMetricPolicy())
	case OverlayConfirmation:
		if fullScreenSurface(model.width, model.height) {
			return confirmationFullScreen(model.jobActions.confirmation, model.width, model.height)
		}
		return model.overlay(base, confirmationPopup(model.jobActions.confirmation, model.width))
	case OverlayJobLog, OverlayJobDiff, OverlayCandidateSource:
		return jobReaderView(base, model.jobReader, model.width, model.height)
	case OverlaySettingsDirty:
		return dirtyChoiceView(base, "UNSAVED SETTINGS", model.configSettings.dirtyCursor, model.width, model.height)
	case OverlayShutdown:
		return shutdownView(base, model.shutdown, model.width, model.height)
	default:
		return base
	}
}

func jobMonitorView(base string, state jobMonitorState, screenWidth, screenHeight int, policy agentMetricPolicy) string {
	width := min(82, max(36, screenWidth-4))
	height := max(6, min(18, screenHeight-6))
	content := state.content(width-4, height, policy)
	items := []string{"Esc close"}
	job := state.job
	if containsFixAction(job.AllowedActions, fix.ActionCancel) {
		items = append(items, "C cancel")
	}
	items = append(items, "l logs", "d diff", "[ prev", "] next")
	footer := strings.Join(items, " · ")
	if responsiveTier(screenWidth, screenHeight) == ResponsiveCompact {
		compact := []string{}
		switch {
		case containsFixAction(job.AllowedActions, fix.ActionCancel):
			compact = append(compact, "C cancel")
		}
		compact = append(compact, "[ prev", "] next", "Esc")
		footer = strings.Join(compact, " · ")
	}
	if fullScreenSurface(screenWidth, screenHeight) {
		lines := []string{fixSurfaceLine("INSPECT", screenWidth, style.SurfaceHeader, style.TextPrimary)}
		lines = append(lines, state.content(screenWidth, max(1, screenHeight-2), policy)...)
		for len(lines) < screenHeight-1 {
			lines = append(lines, fixSurfaceLine("", screenWidth, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, screenWidth, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:screenHeight])
	}
	return overlaySurface(base, style.Popup("INSPECT", content, footer, width), screenWidth, screenHeight, 0)
}

func (state jobMonitorState) content(width, height int, policy agentMetricPolicy) []string {
	if state.loading {
		return fitFixContent([]string{fixSurfaceLine("Loading authoritative job snapshot…", width, style.SurfaceModal, style.TextMuted)}, width, height)
	}
	if state.errorText != "" && state.job.ID == "" {
		return fitFixContent([]string{fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary)}, width, height)
	}
	job := state.job
	lines := jobMonitorHeaderLines(job, width)
	lines = append(lines, jobMonitorDeliveryLines(job, width)...)
	lines = append(lines, jobMonitorUsageLines(job, width)...)
	lines = append(lines, state.focusLines(job, width, policy)...)
	lines = append(lines, jobMonitorIssueLines(job, width)...)
	lines = append(lines, jobMonitorActorLines(job, width)...)
	if state.errorText != "" {
		lines = append(lines, fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
	}
	start := min(max(0, state.offset), max(0, len(lines)-height))
	end := min(len(lines), start+height)
	return fitFixContent(lines[start:end], width, height)
}

func jobMonitorHeaderLines(job fix.JobPresentation, width int) []string {
	return []string{
		fixSurfaceLine("Job: "+string(job.ID), width, style.SurfaceModal, style.TextMuted),
		fixSurfaceLine("Goal: "+cleanAgentText(job.Goal), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("Agent: "+strings.Join(nonemptyStrings(agentHarnessName(job), job.ModelLabel, job.EffortLabel), " · "), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("State: "+agentPhaseText(job)+" · "+nonemptySetting(job.CurrentAction, "No current activity"), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(fmt.Sprintf("Targets: %d", len(job.Targets)), width, style.SurfaceModal, style.TextPrimary),
	}
}

func jobMonitorDeliveryLines(job fix.JobPresentation, width int) []string {
	if !job.DeliveryPlan.Valid() {
		return nil
	}
	workspace := "current files"
	if job.DeliveryPlan.Workspace == fix.WorkspaceWorktree {
		workspace = nonemptySetting(job.WorkspacePath, "separate worktree")
	}
	gitResult := gitModeLabel(job.DeliveryPlan.Git)
	if job.DeliveryPlan.Git != fix.GitLeaveUncommitted {
		gitResult += " · " + publishModeLabel(job.DeliveryPlan.Publish)
	}
	return []string{
		fixSurfaceLine("Files: "+workspace, width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("Git: "+gitResult, width, style.SurfaceModal, style.TextPrimary),
	}
}

func jobMonitorUsageLines(job fix.JobPresentation, width int) []string {
	if job.UsageReported {
		return []string{fixSurfaceLine(fmt.Sprintf("Tokens: input %d · cached %d · output %d · reasoning %d", job.Usage.InputTokens, job.Usage.CachedTokens, job.Usage.OutputTokens, job.Usage.ReasoningTokens), width, style.SurfaceModal, style.TextPrimary)}
	}
	return []string{fixSurfaceLine("Tokens: not reported by this agent", width, style.SurfaceModal, style.TextMuted)}
}

func (state jobMonitorState) focusLines(job fix.JobPresentation, width int, policy agentMetricPolicy) []string {
	if state.focusPath == "" {
		return nil
	}
	lines := []string{fixSurfaceLine("Focused file: "+state.focusPath.String(), width, style.SurfaceModal, style.TextPrimary)}
	for _, target := range job.Targets {
		if target.Path == state.focusPath {
			lines = append(lines, fixSurfaceLine(visibleAgentFileMetrics(target, policy.visible), width, style.SurfaceModal, style.TextPrimary))
		}
	}
	return lines
}

func jobMonitorIssueLines(job fix.JobPresentation, width int) []string {
	if job.Issue == nil {
		return nil
	}
	label := "Attention: "
	if job.Phase == fix.PhaseFailed {
		label = "Failure: "
	}
	return []string{fixSurfaceLine(label+strings.Join(nonemptyStrings(job.Issue.Summary, job.Issue.Detail), " · "), width, style.SurfaceModal, style.TextPrimary)}
}

func jobMonitorActorLines(job fix.JobPresentation, width int) []string {
	if len(job.Actors) == 0 {
		return nil
	}
	lines := []string{fixSurfaceLine("ACTORS", width, style.SurfaceModal, style.TextMuted)}
	for _, actor := range job.Actors {
		prefix := "• "
		if actor.ParentID != "" {
			prefix = "  ↳ "
		}
		lines = append(lines, fixSurfaceLine(prefix+cleanAgentText(actor.ID)+" · "+cleanAgentText(actor.CurrentAction), width, style.SurfaceModal, style.TextPrimary))
	}
	return lines
}

func jobMonitorMaxOffset(state jobMonitorState, width, height int, fullScreen bool) int {
	_, contentHeight := jobMonitorContentSize(width, height, fullScreen)
	return max(0, state.lineCount(state.job)-contentHeight)
}

func jobMonitorContentSize(width, height int, fullScreen bool) (int, int) {
	if fullScreen {
		return width, max(1, height-2)
	}
	popupWidth := min(82, max(36, width-4))
	return popupWidth - 4, max(6, min(18, height-6))
}
func (state jobMonitorState) lineCount(job fix.JobPresentation) int {
	if state.loading || state.errorText != "" && job.ID == "" {
		return 1
	}
	count := 6 // job, goal, agent, state, targets, usage
	if job.DeliveryPlan.Valid() {
		count += 2
	}
	if state.focusPath != "" {
		count++
		for _, target := range job.Targets {
			if target.Path == state.focusPath {
				count++
			}
		}
	}
	if job.Issue != nil {
		count++
	}
	if len(job.Actors) > 0 {
		count += 1 + len(job.Actors)
	}
	if state.errorText != "" {
		count++
	}
	return count
}

func agentHarnessName(job fix.JobPresentation) string {
	label := cleanAgentText(job.ProfileLabel)
	if fields := strings.Fields(label); len(fields) > 0 {
		return strings.ToLower(fields[0])
	}
	if profile := strings.TrimSpace(job.ProfileID); profile != "" {
		return strings.ToLower(strings.SplitN(profile, "-", 2)[0])
	}
	return "agent"
}

func jobReaderView(base string, state jobReaderState, screenWidth, screenHeight int) string {
	title := "JOB DETAILS"
	switch state.kind {
	case OverlayJobLog:
		title = "JOB LOG"
		if state.follow {
			title += " · LIVE"
		} else {
			title += " · PAUSED"
		}
	case OverlayJobDiff:
		title = "CANDIDATE DIFF"
	case OverlayCandidateSource:
		title = "CANDIDATE SOURCE"
	}
	width, height := jobReaderDimensions(state.kind, screenWidth, screenHeight)
	content := state.content(width-4, height)
	footer := "PgUp/PgDn · r refresh · Esc back"
	if state.kind == OverlayJobLog {
		footer = "G follow · Esc back"
	}
	if responsiveTier(screenWidth, screenHeight) == ResponsiveCompact {
		if state.kind == OverlayJobLog {
			footer = "G follow · Esc"
		} else {
			footer = "r refresh · Esc back"
		}
	}
	if fullScreenSurface(screenWidth, screenHeight) {
		lines := []string{fixSurfaceLine(title, screenWidth, style.SurfaceHeader, style.TextPrimary)}
		lines = append(lines, state.content(screenWidth, max(1, screenHeight-2))...)
		for len(lines) < screenHeight-1 {
			lines = append(lines, fixSurfaceLine("", screenWidth, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, screenWidth, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:screenHeight])
	}
	return overlaySurface(base, style.Popup(title, content, footer, width), screenWidth, screenHeight, 0)
}

func (state jobReaderState) content(width, height int) []string {
	lines := []string{fixSurfaceLine("Job: "+string(state.jobID)+func() string {
		if state.path != "" {
			return " · " + state.path.String()
		}
		return ""
	}(), width, style.SurfaceModal, style.TextMuted)}
	if state.loading {
		lines = append(lines, fixSurfaceLine("Loading…", width, style.SurfaceModal, style.TextMuted))
	} else if state.errorText != "" {
		lines = append(lines, fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
	} else if len(state.lines) == 0 {
		lines = append(lines, fixSurfaceLine("No details are available yet", width, style.SurfaceModal, style.TextMuted))
	} else {
		available := jobReaderAvailableLines(height)
		start := min(max(0, state.offset), max(0, len(state.lines)-available))
		for _, line := range state.lines[start:min(len(state.lines), start+available)] {
			visible := ansi.Cut(line, state.horizontal, state.horizontal+width)
			lines = append(lines, fixSurfaceLine(visible, width, style.SurfaceModal, style.TextPrimary))
		}
	}
	if state.truncated {
		message := "Output truncated"
		switch state.kind {
		case OverlayJobDiff:
			message = "More changed files exist · press r to retry loading them"
		case OverlayCandidateSource:
			message = "Preview truncated at the configured candidate byte/line limit"
		}
		lines = append(lines, fixSurfaceLine(message, width, style.SurfaceModal, style.TextMuted))
	}
	return fitFixContent(lines, width, height)
}

func jobReaderDimensions(kind OverlayKind, width, height int) (int, int) {
	if kind == OverlayJobLog {
		return min(140, max(36, width-2)), max(6, min(30, height-5))
	}
	return min(92, max(36, width-4)), max(4, min(20, height-7))
}

func jobReaderAvailableLines(height int) int {
	return max(1, height-2)
}

func (state jobReaderState) pageSize(width, height int, fullScreen bool) int {
	_, contentHeight := jobReaderDimensions(state.kind, width, height)
	if fullScreen {
		contentHeight = max(1, height-2)
	}
	return jobReaderAvailableLines(contentHeight)
}

func (state jobReaderState) maxOffset(pageSize int) int {
	return max(0, len(state.lines)-pageSize)
}

func (state jobReaderState) maxHorizontalOffset(width, height int, fullScreen bool) int {
	contentWidth, _ := jobReaderDimensions(state.kind, width, height)
	if fullScreen {
		contentWidth = width
	} else {
		contentWidth -= 4
	}
	longest := 0
	for _, line := range state.lines {
		longest = max(longest, ansi.StringWidth(line))
	}
	return max(0, longest-contentWidth)
}

func (state *jobReaderState) clamp(width, height int, fullScreen bool) {
	pageSize := state.pageSize(width, height, fullScreen)
	maximum := state.maxOffset(pageSize)
	if state.follow {
		state.offset = maximum
	} else {
		state.offset = min(state.offset, maximum)
	}
	state.horizontal = min(state.horizontal, state.maxHorizontalOffset(width, height, fullScreen))
}
func (state fixDialogState) footer() string {
	if state.choiceOpen {
		if state.choiceField == fixFieldFocus {
			return "Space toggle"
		}
		return "Enter select"
	}
	if state.starting {
		return "Starting fix…"
	}
	if state.loading {
		return ""
	}
	if !state.loading && !state.runnable() {
		_, hasSettings := state.remediationSettingsKind()
		if hasSettings {
			return "R recheck · s settings"
		}
		return "R recheck"
	}
	if state.branchEditing {
		return "Enter apply"
	}
	return "r run"
}

func (state fixDialogState) fieldRows(catalog agent.ProfileCatalog, width int) []string {
	values := state.fieldValues(catalog)
	labels := map[int]string{
		fixFieldTargetScore: "Target score", fixFieldFocus: "Metrics", fixFieldProfile: "Agent",
		fixFieldModel: "Model", fixFieldEffort: "Effort", fixFieldScope: "May edit",
		fixFieldWorkspace: "Work in", fixFieldGit: "Git", fixFieldPublish: "Publish", fixFieldBranch: "Branch name",
	}
	fields := state.visibleFields()
	rows := make([]string, 0, len(fields))
	for _, field := range fields {
		value := values[field]
		prefix := "  "
		if field == state.cursor {
			prefix = "› "
		}
		selected := field == state.cursor
		if state.fieldEditable(field) {
			rows = append(rows, style.FormFieldRow(prefix+labels[field], value, width, 18, selected, fixChoiceField(field)))
		} else {
			rows = append(rows, fixSurfaceLine(prefix+fmt.Sprintf("%-16s", labels[field])+value, width, style.SelectionSurface(selected), style.TextPrimary))
		}
	}
	return rows
}

func (state fixDialogState) overlayChoiceMenu(catalog agent.ProfileCatalog, lines []string, width, height, fieldStart, fieldEnd int) []string {
	fields := state.visibleFields()
	fieldRow := fixFieldPosition(fields, state.choiceField)
	if fieldRow < fieldStart || fieldRow >= fieldEnd {
		return lines
	}
	fieldOffset := 0
	if height > 2 {
		fieldOffset = 1 // target row
	}
	anchorRow := fieldOffset + fieldRow - fieldStart
	if height <= 0 {
		return lines
	}
	menu := state.choiceMenu(width, height, 1)
	if len(menu) == 0 {
		return lines
	}
	// Start at the field and shift upward only when needed. The menu may cover
	// form rows in either direction, but never the dialog border.
	menuTop := min(anchorRow, max(0, height-len(menu)))
	minimumWidth := 1
	values := state.fieldValues(catalog)
	for row := menuTop; row < menuTop+len(menu); row++ {
		position := fieldStart + row - fieldOffset
		if row < fieldOffset || position < 0 || position >= len(fields) {
			continue
		}
		field := fields[position]
		fieldWidth := lipgloss.Width(values[field])
		if fixChoiceField(field) {
			fieldWidth++
		}
		minimumWidth = max(minimumWidth, fieldWidth)
	}
	menu = state.choiceMenu(width, height, minimumWidth)
	menuLeft := min(18, max(0, width-lipgloss.Width(menu[0])))
	return overlayFixLines(lines, menu, menuLeft, menuTop, width, height)
}

func (state fixDialogState) choiceMenu(maximumWidth, maximumHeight, minimumWidth int) []string {
	choices := state.choices(state.choiceField)
	return formChoiceMenu(choices, state.choiceCursor, state.choiceField == fixFieldFocus, maximumWidth, maximumHeight, minimumWidth)
}

func (state fixDialogState) cursorRow() int {
	return fixFieldPosition(state.visibleFields(), state.cursor)
}

func dirtyChoiceView(base, title string, cursor, width, height int) string {
	choices := []string{"Save", "Discard", "Continue editing"}
	content := []string{"Unsaved changes would be lost."}
	for index, choice := range choices {
		prefix := "  "
		if index == cursor {
			prefix = "› "
		}
		content = append(content, style.ModalOption(prefix+choice, index == cursor, min(44, max(24, width-8))))
	}
	footer := "Enter choose · Esc continue editing"
	if responsiveTier(width, height) == ResponsiveCompact {
		footer = "Enter choose · Esc edit"
	}
	if fullScreenSurface(width, height) {
		lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
		for _, line := range content {
			lines = append(lines, fixSurfaceLineANSI(line, width, style.SurfaceModal))
		}
		for len(lines) < height-1 {
			lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:height])
	}
	return overlaySurface(base, style.Popup(title, content, footer, min(52, max(32, width-4))), width, height, 0)
}

func shutdownView(base string, state shutdownState, width, height int) string {
	status := fmt.Sprintf("%d active fix jobs will be canceled before exit.", state.active)
	footer := "Enter cancel all + quit · Esc return"
	if state.pending {
		status = fmt.Sprintf("Canceling and joining %d active fix jobs…", state.active)
		footer = "Shutdown in progress"
	}
	if state.errorText != "" {
		status = "Shutdown incomplete: " + cleanAgentText(state.errorText)
		footer = "Enter retry · Esc return"
	}
	if width >= 24 && height == 2 {
		return joinScreenLines([]string{fixSurfaceLine(status, width, style.SurfaceHeader, style.TextPrimary), fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted)})
	}
	if fullScreenSurface(width, height) {
		lines := []string{fixSurfaceLine("ACTIVE FIX JOBS", width, style.SurfaceHeader, style.TextPrimary), fixSurfaceLine(status, width, style.SurfaceModal, style.TextPrimary)}
		for len(lines) < height-1 {
			lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:height])
	}
	return overlaySurface(base, style.Popup("ACTIVE FIX JOBS", []string{status, "Running jobs do not detach from Slopwatch."}, footer, min(64, max(32, width-4))), width, height, 0)
}

func fixDialogPopup(state fixDialogState, catalog agent.ProfileCatalog, screenWidth, screenHeight int) string {
	width := min(76, max(24, screenWidth-4))
	if screenHeight < 10 {
		contentHeight := max(1, screenHeight-4) // border, title, and footer
		content := state.content(catalog, width-4, contentHeight)
		return style.TightPopup("FIX FILE", content, state.footer(), width)
	}
	contentHeight := max(4, min(12, screenHeight-7))
	content := state.content(catalog, width-4, contentHeight)
	return style.Popup("FIX FILE", content, state.footer(), width)
}

func fixTargetScoreEditorView(base string, state fixDialogState, catalog agent.ProfileCatalog, screenWidth, screenHeight int) string {
	underlay := overlaySurface(base, fixDialogPopup(state, catalog, screenWidth, screenHeight), screenWidth, screenHeight, 0)
	width := min(34, max(26, screenWidth-4))
	input := state.score
	style.ApplyTextInputStyle(&input, true)
	input.Width = min(14, max(8, width-8))
	field := style.InputField(input.View(), input.Width+2)
	content := []string{"Score  " + field}
	if state.scoreError != "" {
		content = append(content, lipgloss.NewStyle().Foreground(style.AccentCritical).Background(style.SurfaceModal).Render(state.scoreError))
	}
	popup := style.Popup("TARGET SCORE", content, "Enter apply · Esc cancel", width)
	if screenHeight < 9 {
		popup = style.TightPopup("TARGET SCORE", content, "Enter apply · Esc cancel", width)
	}
	return overlaySurface(underlay, popup, screenWidth, screenHeight, 0)
}

func (state fixDialogState) content(catalog agent.ProfileCatalog, width, height int) []string {
	fields := state.fieldRows(catalog, width)
	runnable := state.runnable()
	preflight := ""
	if state.hasInput {
		preflight = fixPreflightWarning(state.input)
	}
	cursor := state.cursorRow()
	lines := []string{}
	if height > 2 {
		targetText := "Target: " + state.target.String()
		if len(state.targetPaths()) > 1 {
			targetText = "Targets: " + markedFilesLabel(len(state.targetPaths()))
		}
		lines = append(lines, fixSurfaceLine(targetText, width, style.SurfaceModal, style.TextMuted))
	}
	if state.loading || !state.hasInput {
		message := "PREPARING ANALYSIS…"
		if state.errorText != "" {
			message = "Error: " + state.errorText
		}
		lines = append(lines, fixWrappedLines(message, width, max(1, height-len(lines)), style.TextPrimary)...)
		return fitFixContent(lines, width, height)
	}
	status := state.statusText
	if state.starting {
		status = "STARTING FIX…"
	} else if state.errorText != "" {
		status = "Error: " + state.errorText
	} else if !runnable {
		status = "FIX BLOCKED · " + fixPreflightSummary(state.input)
	} else if preflight != "" {
		status = "READY WITH WARNING · " + preflight
	} else {
		status = "READY TO FIX"
	}
	statusColour := style.TextMuted
	if !runnable {
		statusColour = style.AccentCritical
	}
	statusLines := fixWrappedLines(status, width, min(3, max(1, height-len(lines)-1)), statusColour)
	available := max(1, height-len(lines)-len(statusLines))
	start := min(max(0, cursor-available/2), max(0, len(fields)-available))
	end := min(len(fields), start+available)
	lines = append(lines, fields[start:end]...)
	lines = append(lines, statusLines...)
	lines = fitFixContent(lines, width, height)
	if state.choiceOpen {
		lines = state.overlayChoiceMenu(catalog, lines, width, height, start, end)
	}
	return lines
}

func fixWrappedLines(text string, width, maximum int, foreground lipgloss.Color) []string {
	wrapped := strings.Split(ansi.Wordwrap(cleanAgentText(text), max(1, width), ""), "\n")
	if maximum > 0 && len(wrapped) > maximum {
		wrapped = wrapped[:maximum]
		wrapped[maximum-1] = truncate(wrapped[maximum-1]+"…", width)
	}
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, fixSurfaceLine(line, width, style.SurfaceModal, foreground))
	}
	return lines
}

func fitFixContent(lines []string, width, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
	}
	return lines
}

func (state fixDialogState) fieldValues(catalog agent.ProfileCatalog) map[int]string {
	branch := state.branch.Value()
	if state.branchEditing {
		input := state.branch
		style.ApplyTextInputStyle(&input, true)
		branch = input.View()
	}
	targetScore := formatTargetScore(state.input.TargetScore)
	return map[int]string{
		fixFieldTargetScore: "≤ " + targetScore,
		fixFieldFocus:       state.metricSelectionLabel(),
		fixFieldProfile:     state.agentLabel(catalog),
		fixFieldModel:       agentOptionLabel(state.input.Probe.Capabilities.Models, state.input.Model),
		fixFieldEffort:      agentOptionLabel(state.input.Probe.Capabilities.Efforts, state.input.Effort),
		fixFieldScope:       changeScopeLabel(state.input.ChangeScope),
		fixFieldWorkspace:   workspaceModeLabel(state.input.DeliveryPlan.Workspace),
		fixFieldGit:         gitModeLabel(state.input.DeliveryPlan.Git),
		fixFieldPublish:     publishModeLabel(state.input.DeliveryPlan.Publish),
		fixFieldBranch:      branch,
	}
}

func formChoiceMenu(choices []fixDialogChoice, cursor int, multi bool, maximumWidth, maximumHeight, minimumWidth int) []string {
	if len(choices) == 0 || maximumWidth <= 0 || maximumHeight <= 0 {
		return nil
	}
	desiredInnerWidth := choiceMenuWidth(choices, multi)

	bordered := maximumHeight >= 3 && maximumWidth >= 5
	borderWidth := 0
	if bordered {
		borderWidth = 2
	}
	innerWidth := max(1, min(max(desiredInnerWidth, minimumWidth-borderWidth), maximumWidth-borderWidth))
	visibleRows := min(len(choices), maximumHeight-borderWidth)
	if visibleRows <= 0 {
		return nil
	}
	start := min(max(0, cursor-visibleRows/2), max(0, len(choices)-visibleRows))
	end := start + visibleRows
	rows := make([]string, 0, visibleRows)
	for index := start; index < start+visibleRows; index++ {
		rows = append(rows, renderChoiceMenuRow(choices[index], index, cursor, start, end, len(choices), multi, innerWidth))
	}
	if !bordered {
		return rows
	}
	menu := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(style.AccentInfo).
		Background(style.SurfaceField).
		Render(strings.Join(rows, "\n"))
	return strings.Split(menu, "\n")
}

func choiceMenuWidth(choices []fixDialogChoice, multi bool) int {
	desired := 1
	for _, choice := range choices {
		desired = max(desired, lipgloss.Width("›↓ "+choiceMenuMark(choice, multi)+" "+choice.label))
	}
	return desired
}

func choiceMenuMark(choice fixDialogChoice, multi bool) string {
	if multi {
		if choice.selected {
			return "[x]"
		}
		return "[ ]"
	}
	if choice.selected {
		return "●"
	}
	return "○"
}

func choiceMenuPrefix(index, cursor, start, end, total int) string {
	active := index == cursor
	prefix := "  "
	if active {
		prefix = "› "
	}
	moreAbove, moreBelow := index == start && start > 0, index == end-1 && end < total
	switch {
	case moreAbove && moreBelow:
		prefix = " ↕ "
		if active {
			prefix = "›↕ "
		}
	case moreAbove:
		prefix = " ↑ "
		if active {
			prefix = "›↑ "
		}
	case moreBelow:
		prefix = " ↓ "
		if active {
			prefix = "›↓ "
		}
	}
	return prefix
}

func renderChoiceMenuRow(choice fixDialogChoice, index, cursor, start, end, total int, multi bool, width int) string {
	active := index == cursor
	foreground, background := style.TextPrimary, style.SurfaceField
	if choice.disabled {
		foreground = style.TextMuted
	}
	if active {
		background = style.SurfaceFieldActive
	}
	text := ansi.Truncate(choiceMenuPrefix(index, cursor, start, end, total)+choiceMenuMark(choice, multi)+" "+choice.label, width, "")
	return lipgloss.NewStyle().Width(width).Background(background).Foreground(foreground).Bold(active).Render(text)
}

func overlayFixLines(base, overlay []string, left, top, width, height int) []string {
	result := append([]string(nil), base...)
	for len(result) < height {
		result = append(result, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
	}
	for index, overlayLine := range overlay {
		row := top + index
		if row < 0 || row >= min(height, len(result)) || left >= width {
			continue
		}
		baseLine := padANSI(result[row], width)
		overlayWidth := min(lipgloss.Width(overlayLine), width-left)
		if overlayWidth <= 0 {
			continue
		}
		result[row] = ansi.Cut(baseLine, 0, left) +
			ansi.Cut(padANSI(overlayLine, overlayWidth), 0, overlayWidth) +
			ansi.Cut(baseLine, left+overlayWidth, width)
	}
	return result[:min(height, len(result))]
}

func (state fixDialogState) fieldEditable(field int) bool {
	switch field {
	case fixFieldTargetScore, fixFieldFocus, fixFieldScope, fixFieldWorkspace, fixFieldGit, fixFieldPublish, fixFieldBranch:
		return true
	case fixFieldProfile:
		return len(state.input.Preferences.Profiles) > 1
	case fixFieldModel:
		return len(state.input.Probe.Capabilities.Models) > 0
	case fixFieldEffort:
		return len(state.input.Probe.Capabilities.Efforts) > 1
	default:
		return false
	}
}

func (state fixDialogState) metricSelectionLabel() string {
	labels := make([]string, 0, len(state.metrics))
	for _, id := range state.metrics {
		if state.focus[id] {
			labels = append(labels, fixMetricLabel(id))
		}
	}
	if len(labels) == 0 {
		return "none"
	}
	if len(labels) <= 2 {
		return strings.Join(labels, ", ")
	}
	return fmt.Sprintf("%s + %d", labels[0], len(labels)-1)
}

func (state fixDialogState) visibleFields() []int {
	fields := []int{fixFieldTargetScore}
	if len(state.metrics) > 0 {
		fields = append(fields, fixFieldFocus)
	}
	fields = append(fields, fixFieldProfile, fixFieldModel, fixFieldEffort)
	fields = append(fields, fixFieldScope)
	fields = append(fields, fixFieldWorkspace, fixFieldGit)
	if state.input.DeliveryPlan.Git != fix.GitLeaveUncommitted {
		fields = append(fields, fixFieldPublish)
	}
	if state.input.DeliveryPlan.Git == fix.GitCommitNewBranch {
		fields = append(fields, fixFieldBranch)
	}
	return fields
}

func fixFieldPosition(fields []int, field int) int {
	for index, candidate := range fields {
		if candidate == field {
			return index
		}
	}
	return 0
}

func (state fixDialogState) agentLabel(catalog agent.ProfileCatalog) string {
	profile := state.input.Profile
	if catalog != nil {
		if descriptor, err := catalog.Descriptor(profile.Runtime); err == nil && descriptor.Label != "" {
			return cleanAgentText(descriptor.Label)
		}
	}
	switch profile.Runtime {
	case "codex-cli":
		return "Codex"
	case "openai-responses":
		return "OpenAI API"
	}
	label := cleanAgentText(profile.Label)
	for _, separator := range []string{" —", " ·", " ("} {
		if before, _, found := strings.Cut(label, separator); found {
			label = before
		}
	}
	return nonemptySetting(label, string(profile.ID))
}

func agentOptionLabel[T ~string](options []agent.Option[T], selected T) string {
	if selected == "" {
		return "Default"
	}
	for _, option := range options {
		if option.ID == selected {
			return nonemptySetting(cleanAgentText(option.Label), string(option.ID))
		}
	}
	return string(selected)
}

func changeScopeLabel(value string) string {
	switch value {
	case "targets-only":
		return "Selected files"
	case "repository":
		return "Any file in the project"
	default:
		return "Selected files + related tests"
	}
}

func workspaceModeLabel(value fix.WorkspaceMode) string {
	switch value {
	case fix.WorkspaceWorktree:
		return "Separate worktree"
	default:
		return "Current files"
	}
}

func gitModeLabel(value fix.GitMode) string {
	switch value {
	case fix.GitCommitCurrent:
		return "Commit current branch"
	case fix.GitCommitNewBranch:
		return "Commit new branch"
	default:
		return "Leave uncommitted"
	}
}

func publishModeLabel(value fix.PublishMode) string {
	switch value {
	case fix.PublishPush:
		return "Push"
	case fix.PublishPullRequest:
		return "Open pull request"
	default:
		return "Keep local"
	}
}

func fixMetricLabel(id fix.MetricID) string {
	if id == "score" {
		return "SCORE"
	}
	definition, ok := scoring.MetricDefinitionByID(scoring.MetricID(id))
	if ok && definition.ComponentID != "" {
		if component, found := scoring.ComponentByID(definition.ComponentID); found {
			return component.Label
		}
	}
	if id == fix.MetricID(scoring.MetricTypeSafety) {
		return "Type safety"
	}
	return strings.ToUpper(string(id))
}

func fixOptionContains[T ~string](options []agent.Option[T], wanted T) bool {
	for _, option := range options {
		if option.ID == wanted {
			return true
		}
	}
	return false
}

func fixPreflightSummary(input fixapp.FixInput) string {
	if warning := fixPreflightWarning(input); warning != "" {
		return warning
	}
	return "the input is incomplete"
}

func fixPreflightWarning(input fixapp.FixInput) string {
	if warning := deliveryPreflightWarning(input); warning != "" {
		return warning
	}
	return probePreflightWarning(input)
}

func deliveryPreflightWarning(input fixapp.FixInput) string {
	checks := []struct {
		invalid bool
		message string
	}{
		{input.DeliveryPlan.Git == fix.GitCommitNewBranch && strings.TrimSpace(input.BranchName) == "", "enter a branch name"},
		{input.DeliveryPlan.Git != fix.GitLeaveUncommitted && input.Workspace.GitCommonDir == "", "this folder is not a Git repository; choose Leave uncommitted"},
		{input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && input.DeliveryPlan.Git == fix.GitCommitCurrent, "choose Current files to commit the current branch"},
		{input.DeliveryPlan.Git == fix.GitCommitCurrent && input.Workspace.CurrentBranch == "", "Git has no current branch; leave changes uncommitted or create a new branch"},
	}
	for _, check := range checks {
		if check.invalid {
			return check.message
		}
	}
	return ""
}

func probePreflightWarning(input fixapp.FixInput) string {
	if input.Probe.State == "" {
		return ""
	}
	diagnostic := cleanAgentText(input.Probe.Diagnostic)
	suffix := ""
	if diagnostic != "" {
		suffix = " · " + diagnostic
	}
	if warning := probeStateWarning(input.Probe.State, suffix); warning != "" {
		return warning
	}
	return probeCapabilityWarning(input)
}

func probeStateWarning(state agent.ProbeState, suffix string) string {
	switch state {
	case "":
		return ""
	case agent.ProbeUnauthenticated:
		return "agent appears unauthenticated; the job will attempt to connect at runtime" + suffix
	case agent.ProbeUnavailable, agent.ProbeIncompatible:
		return fmt.Sprintf("agent appears %s; the job will attempt to start it at runtime%s", state, suffix)
	case agent.ProbeDegraded:
		return "agent readiness is degraded; the job will report any concrete runtime failure" + suffix
	case agent.ProbeReady:
		return ""
	default:
		return fmt.Sprintf("agent readiness is %s; the job will attempt to start it at runtime%s", state, suffix)
	}
}

func probeCapabilityWarning(input fixapp.FixInput) string {
	if !input.Probe.Capabilities.Isolation.EligibleForMutation() {
		return fixAgentName(input) + " did not report the configured isolation capabilities"
	}
	if !fixOptionContains(input.Probe.Capabilities.Models, input.Model) ||
		!fixOptionContains(input.Probe.Capabilities.Efforts, input.Effort) {
		return "selected model or effort was not reported by the readiness probe"
	}
	return ""
}

func fixAgentName(input fixapp.FixInput) string {
	if label := cleanAgentText(input.Profile.Label); label != "" {
		return label
	}
	if runtime := cleanAgentText(string(input.Profile.Runtime)); runtime != "" {
		return runtime
	}
	return "The selected agent"
}

func masterPromptEditorView(state configSettingsState, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	errorText := cleanAgentText(state.promptError)
	title := "MASTER AGENT PROMPT"
	footer := "Ctrl-S done · Esc cancel"
	lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
	for _, line := range strings.Split(state.prompt.View(), "\n") {
		lines = append(lines, fixSurfaceLineANSI(line, width, style.SurfaceFieldActive))
	}
	if errorText != "" {
		lines = append(lines, fixSurfaceLine(errorText, width, style.SurfaceModal, style.AccentCritical))
	}
	for len(lines) < height-1 {
		lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:height])
}

func confirmationPopup(state cancelConfirmation, width int) string {
	status := jobActionConfirmationQuestion(state.action)
	if state.pending {
		status = "Requesting " + strings.ToLower(jobActionLabel(state.action)) + "…"
	}
	content := []string{"Job: " + string(state.jobID), status}
	content = append(content, jobActionOutcomeLines(state)...)
	if state.errorText != "" {
		content = append(content, "Error: "+cleanAgentText(state.errorText))
	}
	footer := "Enter confirm · Esc stay"
	if !state.allowed {
		footer = "Esc back · cancel unavailable"
	}
	return style.Popup("CONFIRM "+strings.ToUpper(jobActionLabel(state.action)), content, footer, min(68, max(32, width-4)))
}

func confirmationFullScreen(state cancelConfirmation, width, height int) string {
	lines := []string{fixSurfaceLine("CONFIRM "+strings.ToUpper(jobActionLabel(state.action)), width, style.SurfaceHeader, style.TextPrimary)}
	body := []string{
		fixSurfaceLine("Job: "+string(state.jobID), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(jobActionConfirmationQuestion(state.action), width, style.SurfaceModal, style.TextPrimary),
	}
	for _, value := range jobActionOutcomeLines(state) {
		body = append(body, fixSurfaceLine(value, width, style.SurfaceModal, style.TextPrimary))
	}
	if state.errorText != "" {
		body = append(body, fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
	}
	available := max(0, height-2)
	if len(body) > available {
		body = body[:available]
	}
	lines = append(lines, body...)
	for len(lines) < height-1 {
		lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
	}
	footer := "Enter confirm · Esc stay"
	if !state.allowed {
		footer = "Esc back · cancel unavailable"
	}
	lines = append(lines, fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:height])
}

func jobActionLabel(action fix.JobAction) string {
	if action == fix.ActionCancel {
		return "Cancel"
	}
	return cleanAgentText(string(action))
}

func jobActionConfirmationQuestion(action fix.JobAction) string {
	if action == fix.ActionCancel {
		return "Cancel only this job?"
	}
	return "Apply " + strings.ToLower(jobActionLabel(action)) + "?"
}

func jobActionOutcomeLines(state cancelConfirmation) []string {
	return nil
}

func fixSurfaceLine(text string, width int, background, foreground lipgloss.Color) string {
	return lipgloss.NewStyle().Background(background).Foreground(foreground).
		Render(padANSI(truncate(cleanAgentText(text), width), width))
}

func fixSurfaceLineANSI(text string, width int, background lipgloss.Color) string {
	return lipgloss.NewStyle().Background(background).Render(padANSI(truncateANSI(text, width), width))
}

// cleanEditorText removes terminal control input before a widget is allowed to
// add its own trusted cursor/style sequences.
func cleanEditorText(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(character rune) rune {
		switch character {
		case '\n', '\t':
			return character
		case '\r':
			return '\n'
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}
