package follow

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixapp"
	"github.com/blater/slopmochi/internal/scoring"
	"github.com/blater/slopmochi/internal/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (model Model) featureOverlayView(base string, frame OverlayFrame) string {
	switch frame.Kind {
	case OverlayFixForm:
		return model.overlay(base, model.fixDialogPopup())
	case OverlayTargetScoreEditor:
		return model.fixTargetScoreEditorView(base)
	case OverlayConfigSettings:
		if fullScreenSurface(model.width, model.height) {
			return model.configSettingsFullScreen()
		}
		return model.overlay(base, model.configSettingsView())
	case OverlayPromptEditor:
		return model.masterPromptEditorView()
	case OverlayJobMonitor:
		return model.jobMonitorView(base)
	case OverlayConfirmation:
		if fullScreenSurface(model.width, model.height) {
			return model.confirmationFullScreen()
		}
		return model.overlay(base, model.confirmationPopup())
	case OverlayJobLog, OverlayJobDiff, OverlayCandidateSource:
		return model.jobReaderView(base)
	case OverlaySettingsDirty:
		return model.dirtyChoiceView(base, "UNSAVED SETTINGS", model.configSettings.dirtyCursor)
	case OverlayShutdown:
		return model.shutdownView(base)
	default:
		return base
	}
}

func (model Model) jobMonitorView(base string) string {
	width := min(82, max(36, model.width-4))
	height := max(6, min(18, model.height-6))
	content := model.jobMonitorContent(width-4, height)
	items := []string{"Esc close"}
	job := model.jobMonitor.job
	if containsFixAction(job.AllowedActions, fix.ActionCancel) {
		items = append(items, "C cancel")
	}
	items = append(items, "l logs", "d diff", "[ prev", "] next")
	footer := strings.Join(items, " · ")
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		compact := []string{}
		switch {
		case containsFixAction(job.AllowedActions, fix.ActionCancel):
			compact = append(compact, "C cancel")
		}
		compact = append(compact, "[ prev", "] next", "Esc")
		footer = strings.Join(compact, " · ")
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine("INSPECT", model.width, style.SurfaceHeader, style.TextPrimary)}
		lines = append(lines, model.jobMonitorContent(model.width, max(1, model.height-2))...)
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup("INSPECT", content, footer, width))
}

func (model Model) jobMonitorContent(width, height int) []string {
	state := model.jobMonitor
	if state.loading {
		return fitFixContent([]string{fixSurfaceLine("Loading authoritative job snapshot…", width, style.SurfaceModal, style.TextMuted)}, width, height)
	}
	if state.errorText != "" && state.job.ID == "" {
		return fitFixContent([]string{fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary)}, width, height)
	}
	job := state.job
	lines := []string{
		fixSurfaceLine("Job: "+string(job.ID), width, style.SurfaceModal, style.TextMuted),
		fixSurfaceLine("Goal: "+cleanAgentText(job.Goal), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("Agent: "+strings.Join(nonemptyStrings(agentHarnessName(job), job.ModelLabel, job.EffortLabel), " · "), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("State: "+agentPhaseText(job)+" · "+nonemptySetting(job.CurrentAction, "No current activity"), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(fmt.Sprintf("Targets: %d", len(job.Targets)), width, style.SurfaceModal, style.TextPrimary),
	}
	if job.DeliveryPlan.Valid() {
		workspace := "current files"
		if job.DeliveryPlan.Workspace == fix.WorkspaceWorktree {
			workspace = nonemptySetting(job.WorkspacePath, "separate worktree")
		}
		lines = append(lines, fixSurfaceLine("Files: "+workspace, width, style.SurfaceModal, style.TextPrimary))
		gitResult := gitModeLabel(job.DeliveryPlan.Git)
		if job.DeliveryPlan.Git != fix.GitLeaveUncommitted {
			gitResult += " · " + publishModeLabel(job.DeliveryPlan.Publish)
		}
		lines = append(lines, fixSurfaceLine("Git: "+gitResult, width, style.SurfaceModal, style.TextPrimary))
	}
	if job.UsageReported {
		lines = append(lines, fixSurfaceLine(fmt.Sprintf("Tokens: input %d · cached %d · output %d · reasoning %d", job.Usage.InputTokens, job.Usage.CachedTokens, job.Usage.OutputTokens, job.Usage.ReasoningTokens), width, style.SurfaceModal, style.TextPrimary))
	} else {
		lines = append(lines, fixSurfaceLine("Tokens: not reported by this agent", width, style.SurfaceModal, style.TextMuted))
	}
	if state.focusPath != "" {
		lines = append(lines, fixSurfaceLine("Focused file: "+state.focusPath.String(), width, style.SurfaceModal, style.TextPrimary))
		for _, target := range job.Targets {
			if target.Path == state.focusPath {
				lines = append(lines, fixSurfaceLine(model.visibleAgentFileMetrics(target), width, style.SurfaceModal, style.TextPrimary))
			}
		}
	}
	if job.Issue != nil {
		label := "Attention: "
		if job.Phase == fix.PhaseFailed {
			label = "Failure: "
		}
		lines = append(lines, fixSurfaceLine(label+strings.Join(nonemptyStrings(job.Issue.Summary, job.Issue.Detail), " · "), width, style.SurfaceModal, style.TextPrimary))
	}
	if len(job.Actors) > 0 {
		lines = append(lines, fixSurfaceLine("ACTORS", width, style.SurfaceModal, style.TextMuted))
		for _, actor := range job.Actors {
			prefix := "• "
			if actor.ParentID != "" {
				prefix = "  ↳ "
			}
			lines = append(lines, fixSurfaceLine(prefix+cleanAgentText(actor.ID)+" · "+cleanAgentText(actor.CurrentAction), width, style.SurfaceModal, style.TextPrimary))
		}
	}
	if state.errorText != "" {
		lines = append(lines, fixSurfaceLine("Error: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
	}
	start := min(max(0, state.offset), max(0, len(lines)-height))
	end := min(len(lines), start+height)
	return fitFixContent(lines[start:end], width, height)
}

func (model Model) jobMonitorMaxOffset() int {
	_, height := model.jobMonitorContentSize()
	return max(0, model.jobMonitorLineCount()-height)
}

func (model Model) jobMonitorContentSize() (int, int) {
	if fullScreenSurface(model.width, model.height) {
		return model.width, max(1, model.height-2)
	}
	width := min(82, max(36, model.width-4))
	return width - 4, max(6, min(18, model.height-6))
}

func (model Model) jobMonitorLineCount() int {
	state, job := model.jobMonitor, model.jobMonitor.job
	if state.loading || state.errorText != "" && job.ID == "" {
		return 1
	}
	count := 6 // job, goal, agent, state, targets, usage
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

func (model Model) jobReaderView(base string) string {
	state := model.jobReader
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
	width, height := model.jobReaderDimensions()
	content := model.jobReaderContent(width-4, height)
	footer := "PgUp/PgDn · r refresh · Esc back"
	if state.kind == OverlayJobLog {
		footer = "G follow · Esc back"
	}
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		if state.kind == OverlayJobLog {
			footer = "G follow · Esc"
		} else {
			footer = "r refresh · Esc back"
		}
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine(title, model.width, style.SurfaceHeader, style.TextPrimary)}
		lines = append(lines, model.jobReaderContent(model.width, max(1, model.height-2))...)
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup(title, content, footer, width))
}

func (model Model) jobReaderContent(width, height int) []string {
	state := model.jobReader
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

func (model Model) jobReaderDimensions() (int, int) {
	if model.jobReader.kind == OverlayJobLog {
		return min(140, max(36, model.width-2)), max(6, min(30, model.height-5))
	}
	return min(92, max(36, model.width-4)), max(4, min(20, model.height-7))
}

func jobReaderAvailableLines(height int) int {
	return max(1, height-2)
}

func (model Model) jobReaderPageSize() int {
	_, height := model.jobReaderDimensions()
	if fullScreenSurface(model.width, model.height) {
		height = max(1, model.height-2)
	}
	return jobReaderAvailableLines(height)
}

func (model Model) jobReaderMaxOffset() int {
	return max(0, len(model.jobReader.lines)-model.jobReaderPageSize())
}

func (model Model) jobReaderMaxHorizontalOffset() int {
	width, _ := model.jobReaderDimensions()
	if fullScreenSurface(model.width, model.height) {
		width = model.width
	} else {
		width -= 4
	}
	longest := 0
	for _, line := range model.jobReader.lines {
		longest = max(longest, ansi.StringWidth(line))
	}
	return max(0, longest-width)
}

func (model *Model) clampJobReaderPosition() {
	if model.jobReader.follow {
		model.jobReader.offset = model.jobReaderMaxOffset()
	} else {
		model.jobReader.offset = min(model.jobReader.offset, model.jobReaderMaxOffset())
	}
	model.jobReader.horizontal = min(model.jobReader.horizontal, model.jobReaderMaxHorizontalOffset())
}

func (model Model) dirtyChoiceView(base, title string, cursor int) string {
	choices := []string{"Save", "Discard", "Continue editing"}
	content := []string{"Unsaved changes would be lost."}
	for index, choice := range choices {
		prefix := "  "
		if index == cursor {
			prefix = "› "
		}
		content = append(content, style.ModalOption(prefix+choice, index == cursor, min(44, max(24, model.width-8))))
	}
	footer := "Enter choose · Esc continue editing"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "Enter choose · Esc edit"
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine(title, model.width, style.SurfaceHeader, style.TextPrimary)}
		for _, line := range content {
			lines = append(lines, fixSurfaceLineANSI(line, model.width, style.SurfaceModal))
		}
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup(title, content, footer, min(52, max(32, model.width-4))))
}

func (model Model) shutdownView(base string) string {
	state := model.shutdown
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
	if model.width >= 24 && model.height == 2 {
		return joinScreenLines([]string{fixSurfaceLine(status, model.width, style.SurfaceHeader, style.TextPrimary), fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted)})
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine("ACTIVE FIX JOBS", model.width, style.SurfaceHeader, style.TextPrimary), fixSurfaceLine(status, model.width, style.SurfaceModal, style.TextPrimary)}
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup("ACTIVE FIX JOBS", []string{status, "Running jobs do not detach from Slopmochi."}, footer, min(64, max(32, model.width-4))))
}

func (model Model) fixDialogPopup() string {
	width := min(76, max(24, model.width-4))
	if model.height < 10 {
		contentHeight := max(1, model.height-4) // border, title, and footer
		content := model.fixDialogContent(width-4, contentHeight)
		return style.TightPopup("FIX FILE", content, model.fixDialogFooter(), width)
	}
	contentHeight := max(4, min(12, model.height-7))
	content := model.fixDialogContent(width-4, contentHeight)
	return style.Popup("FIX FILE", content, model.fixDialogFooter(), width)
}

func (model Model) fixDialogFooter() string {
	if model.fixDialog.choiceOpen {
		if model.fixDialog.choiceField == fixFieldFocus {
			return "Space toggle"
		}
		return "Enter select"
	}
	if model.fixDialog.starting {
		return "Starting fix…"
	}
	if model.fixDialog.loading {
		return ""
	}
	if !model.fixDialog.loading && !model.fixDialogRunnable() {
		_, hasSettings := model.fixRemediationSettingsKind()
		if hasSettings {
			return "R recheck · s settings"
		}
		return "R recheck"
	}
	if model.fixDialog.branchEditing {
		return "Enter apply"
	}
	return "r run"
}

func (model Model) fixTargetScoreEditorView(base string) string {
	underlay := model.overlay(base, model.fixDialogPopup())
	width := min(34, max(26, model.width-4))
	input := model.fixDialog.score
	style.ApplyTextInputStyle(&input, true)
	input.Width = min(14, max(8, width-8))
	field := style.InputField(input.View(), input.Width+2)
	content := []string{"Score  " + field}
	if model.fixDialog.scoreError != "" {
		content = append(content, lipgloss.NewStyle().Foreground(style.AccentCritical).Background(style.SurfaceModal).Render(model.fixDialog.scoreError))
	}
	popup := style.Popup("TARGET SCORE", content, "Enter apply · Esc cancel", width)
	if model.height < 9 {
		popup = style.TightPopup("TARGET SCORE", content, "Enter apply · Esc cancel", width)
	}
	return model.overlay(underlay, popup)
}

func (model Model) fixDialogContent(width, height int) []string {
	state := model.fixDialog
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
	} else if !model.fixDialogRunnable() {
		status = "FIX BLOCKED · " + fixPreflightSummary(state.input)
	} else if warning := fixPreflightWarning(state.input); warning != "" {
		status = "READY WITH WARNING · " + warning
	} else {
		status = "READY TO FIX"
	}
	statusColour := style.TextMuted
	if !model.fixDialogRunnable() {
		statusColour = style.AccentCritical
	}
	statusLines := fixWrappedLines(status, width, min(3, max(1, height-len(lines)-1)), statusColour)
	fields := model.fixFieldRows(width)
	available := max(1, height-len(lines)-len(statusLines))
	cursor := model.fixDialogCursorRow()
	start := min(max(0, cursor-available/2), max(0, len(fields)-available))
	end := min(len(fields), start+available)
	lines = append(lines, fields[start:end]...)
	lines = append(lines, statusLines...)
	lines = fitFixContent(lines, width, height)
	if state.choiceOpen {
		lines = model.overlayFixChoiceMenu(lines, width, height, start, end)
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

func (model Model) fixFieldRows(width int) []string {
	state := model.fixDialog
	values := model.fixFieldValues()
	labels := map[int]string{
		fixFieldTargetScore: "Target score", fixFieldFocus: "Metrics", fixFieldProfile: "Agent",
		fixFieldModel: "Model", fixFieldEffort: "Effort", fixFieldScope: "May edit",
		fixFieldWorkspace: "Work in", fixFieldGit: "Git", fixFieldPublish: "Publish", fixFieldBranch: "Branch name",
	}
	fields := model.fixVisibleFields()
	rows := make([]string, 0, len(fields))
	for _, field := range fields {
		value := values[field]
		prefix := "  "
		if field == state.cursor {
			prefix = "› "
		}
		selected := field == state.cursor
		if model.fixFieldEditable(field) {
			rows = append(rows, style.FormFieldRow(prefix+labels[field], value, width, 18, selected, fixChoiceField(field)))
		} else {
			rows = append(rows, fixSurfaceLine(prefix+fmt.Sprintf("%-16s", labels[field])+value, width, style.SelectionSurface(selected), style.TextPrimary))
		}
	}
	return rows
}

func (model Model) fixFieldValues() map[int]string {
	state := model.fixDialog
	branch := state.branch.Value()
	if state.branchEditing {
		input := state.branch
		style.ApplyTextInputStyle(&input, true)
		branch = input.View()
	}
	targetScore := formatTargetScore(state.input.TargetScore)
	return map[int]string{
		fixFieldTargetScore: "≤ " + targetScore,
		fixFieldFocus:       model.fixMetricSelectionLabel(),
		fixFieldProfile:     model.fixAgentLabel(),
		fixFieldModel:       agentOptionLabel(state.input.Probe.Capabilities.Models, state.input.Model),
		fixFieldEffort:      agentOptionLabel(state.input.Probe.Capabilities.Efforts, state.input.Effort),
		fixFieldScope:       changeScopeLabel(state.input.ChangeScope),
		fixFieldWorkspace:   workspaceModeLabel(state.input.DeliveryPlan.Workspace),
		fixFieldGit:         gitModeLabel(state.input.DeliveryPlan.Git),
		fixFieldPublish:     publishModeLabel(state.input.DeliveryPlan.Publish),
		fixFieldBranch:      branch,
	}
}

func (model Model) overlayFixChoiceMenu(lines []string, width, height, fieldStart, fieldEnd int) []string {
	state := model.fixDialog
	fields := model.fixVisibleFields()
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
	menu := model.fixChoiceMenu(width, height, 1)
	if len(menu) == 0 {
		return lines
	}
	// Start at the field and shift upward only when needed. The menu may cover
	// form rows in either direction, but never the dialog border.
	menuTop := min(anchorRow, max(0, height-len(menu)))
	minimumWidth := 1
	values := model.fixFieldValues()
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
	menu = model.fixChoiceMenu(width, height, minimumWidth)
	menuLeft := min(18, max(0, width-lipgloss.Width(menu[0])))
	return overlayFixLines(lines, menu, menuLeft, menuTop, width, height)
}

func (model Model) fixChoiceMenu(maximumWidth, maximumHeight, minimumWidth int) []string {
	choices := model.fixChoices(model.fixDialog.choiceField)
	return formChoiceMenu(choices, model.fixDialog.choiceCursor, model.fixDialog.choiceField == fixFieldFocus, maximumWidth, maximumHeight, minimumWidth)
}

func formChoiceMenu(choices []fixDialogChoice, cursor int, multi bool, maximumWidth, maximumHeight, minimumWidth int) []string {
	if len(choices) == 0 || maximumWidth <= 0 || maximumHeight <= 0 {
		return nil
	}
	desiredInnerWidth := 1
	for _, choice := range choices {
		mark := "○"
		if choice.selected {
			mark = "●"
		}
		if multi {
			mark = "[ ]"
			if choice.selected {
				mark = "[x]"
			}
		}
		desiredInnerWidth = max(desiredInnerWidth, lipgloss.Width("›↓ "+mark+" "+choice.label))
	}

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
		choice := choices[index]
		active := index == cursor
		mark := "○"
		if choice.selected {
			mark = "●"
		}
		if multi {
			mark = "[ ]"
			if choice.selected {
				mark = "[x]"
			}
		}
		prefix := "  "
		if active {
			prefix = "› "
		}
		moreAbove := index == start && start > 0
		moreBelow := index == end-1 && end < len(choices)
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
		foreground := style.TextPrimary
		if choice.disabled {
			foreground = style.TextMuted
		}
		background := style.SurfaceField
		if active {
			background = style.SurfaceFieldActive
		}
		text := ansi.Truncate(prefix+mark+" "+choice.label, innerWidth, "")
		rows = append(rows, lipgloss.NewStyle().Width(innerWidth).Background(background).Foreground(foreground).Bold(active).Render(text))
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

func (model Model) fixFieldEditable(field int) bool {
	state := model.fixDialog
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

func (model Model) fixMetricSelectionLabel() string {
	state := model.fixDialog
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

func (model Model) fixDialogCursorRow() int {
	return fixFieldPosition(model.fixVisibleFields(), model.fixDialog.cursor)
}

func (model Model) fixVisibleFields() []int {
	state := model.fixDialog
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

func (model Model) fixAgentLabel() string {
	profile := model.fixDialog.input.Profile
	if model.profileCatalog != nil {
		if descriptor, err := model.profileCatalog.Descriptor(profile.Runtime); err == nil && descriptor.Label != "" {
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
	if input.DeliveryPlan.Git == fix.GitCommitNewBranch && strings.TrimSpace(input.BranchName) == "" {
		return "enter a branch name"
	}
	if input.DeliveryPlan.Git != fix.GitLeaveUncommitted && input.Workspace.GitCommonDir == "" {
		return "this folder is not a Git repository; choose Leave uncommitted"
	}
	if input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && input.DeliveryPlan.Git == fix.GitCommitCurrent {
		return "choose Current files to commit the current branch"
	}
	if input.DeliveryPlan.Git == fix.GitCommitCurrent && input.Workspace.CurrentBranch == "" {
		return "Git has no current branch; leave changes uncommitted or create a new branch"
	}
	diagnostic := cleanAgentText(input.Probe.Diagnostic)
	suffix := ""
	if diagnostic != "" {
		suffix = " · " + diagnostic
	}
	switch input.Probe.State {
	case "":
		return ""
	case agent.ProbeUnauthenticated:
		return "agent appears unauthenticated; the job will attempt to connect at runtime" + suffix
	case agent.ProbeUnavailable, agent.ProbeIncompatible:
		return fmt.Sprintf("agent appears %s; the job will attempt to start it at runtime%s", input.Probe.State, suffix)
	case agent.ProbeDegraded:
		return "agent readiness is degraded; the job will report any concrete runtime failure" + suffix
	default:
		if input.Probe.State != agent.ProbeReady {
			return fmt.Sprintf("agent readiness is %s; the job will attempt to start it at runtime%s", input.Probe.State, suffix)
		}
	}
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

func (model Model) masterPromptEditorView() string {
	width, height := model.width, model.height
	if width <= 0 || height <= 0 {
		return ""
	}
	errorText := cleanAgentText(model.configSettings.promptError)
	title := "MASTER AGENT PROMPT"
	footer := "Ctrl-S done · Esc cancel"
	lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
	for _, line := range strings.Split(model.configSettings.prompt.View(), "\n") {
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

func (model Model) confirmationPopup() string {
	state := model.cancelConfirmation
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
	return style.Popup("CONFIRM "+strings.ToUpper(jobActionLabel(state.action)), content, footer, min(68, max(32, model.width-4)))
}

func (model Model) confirmationFullScreen() string {
	lines := []string{fixSurfaceLine("CONFIRM "+strings.ToUpper(jobActionLabel(model.cancelConfirmation.action)), model.width, style.SurfaceHeader, style.TextPrimary)}
	body := []string{
		fixSurfaceLine("Job: "+string(model.cancelConfirmation.jobID), model.width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(jobActionConfirmationQuestion(model.cancelConfirmation.action), model.width, style.SurfaceModal, style.TextPrimary),
	}
	for _, value := range jobActionOutcomeLines(model.cancelConfirmation) {
		body = append(body, fixSurfaceLine(value, model.width, style.SurfaceModal, style.TextPrimary))
	}
	if model.cancelConfirmation.errorText != "" {
		body = append(body, fixSurfaceLine("Error: "+model.cancelConfirmation.errorText, model.width, style.SurfaceModal, style.TextPrimary))
	}
	available := max(0, model.height-2)
	if len(body) > available {
		body = body[:available]
	}
	lines = append(lines, body...)
	for len(lines) < model.height-1 {
		lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
	}
	footer := "Enter confirm · Esc stay"
	if !model.cancelConfirmation.allowed {
		footer = "Esc back · cancel unavailable"
	}
	lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:model.height])
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
