package follow

import (
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
)

func (model Model) featureOverlayView(base string, frame OverlayFrame) string {
	switch frame.Kind {
	case OverlayFixForm:
		if fullScreenSurface(model.width, model.height) {
			return model.fixDialogFullScreen()
		}
		return model.overlay(base, model.fixDialogPopup())
	case OverlayPromptEditor:
		return model.promptEditorView()
	case OverlayPromptDetach:
		return model.promptDetachView(base)
	case OverlayPromptDirty:
		return model.dirtyChoiceView(base, "UNSAVED TASK EDITS", model.fixDialog.promptDirtyCursor)
	case OverlayJobMonitor:
		return model.jobMonitorView(base)
	case OverlayJobActions:
		if fullScreenSurface(model.width, model.height) {
			return model.jobActionsFullScreen()
		}
		return model.overlay(base, model.jobActionsPopup())
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
	footer := "Esc background · Enter actions · [/] job · l logs · d diff · C cancel"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "Esc back · [/] job · Enter actions"
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine("FIX JOB · "+agentPhaseText(model.jobMonitor.job), model.width, style.SurfaceHeader, style.TextPrimary)}
		lines = append(lines, model.jobMonitorContent(model.width, max(1, model.height-2))...)
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup("FIX JOB · "+agentPhaseText(model.jobMonitor.job), content, footer, width))
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
		fixSurfaceLine("Agent: "+strings.Join(nonemptyStrings(job.ProfileLabel, job.ModelLabel, job.EffortLabel), " / "), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("State: "+agentPhaseText(job)+" · "+nonemptySetting(job.CurrentAction, "No current activity"), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(fmt.Sprintf("Targets: %d · Compliance: %s · Validation: %s · Scope: %s", len(job.Targets), job.Compliance, job.Validation, job.Scope), width, style.SurfaceModal, style.TextPrimary),
	}
	if state.focusPath != "" {
		lines = append(lines, fixSurfaceLine("Focused file: "+state.focusPath.String(), width, style.SurfaceModal, style.TextPrimary))
		for _, target := range job.Targets {
			if target.Path == state.focusPath {
				lines = append(lines, fixSurfaceLine(agentFileMetrics(target), width, style.SurfaceModal, style.TextPrimary))
			}
		}
	}
	if job.Issue != nil {
		lines = append(lines, fixSurfaceLine("Attention: "+job.Issue.Summary+" · "+job.Issue.Detail, width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine("ACTIVITY", width, style.SurfaceModal, style.TextMuted))
	activity := state.activity
	if len(activity) == 0 {
		lines = append(lines, fixSurfaceLine("No provider activity recorded yet", width, style.SurfaceModal, style.TextMuted))
	} else {
		start := min(max(0, state.offset), max(0, len(activity)-1))
		for _, entry := range activity[start:] {
			lines = append(lines, fixSurfaceLine(entry.At.Format("15:04:05")+"  "+cleanAgentText(entry.Summary), width, style.SurfaceModal, style.TextPrimary))
		}
	}
	if state.errorText != "" {
		lines = append(lines, fixSurfaceLine("Activity warning: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
	}
	return fitFixContent(lines, width, height)
}

func (model Model) jobReaderView(base string) string {
	state := model.jobReader
	title := "JOB DETAILS"
	switch state.kind {
	case OverlayJobLog:
		title = "JOB LOG"
	case OverlayJobDiff:
		title = "CANDIDATE DIFF"
	case OverlayCandidateSource:
		title = "CANDIDATE SOURCE"
	}
	width := min(92, max(36, model.width-4))
	height := max(4, min(20, model.height-7))
	content := model.jobReaderContent(width-4, height)
	footer := "↑/↓ scroll · PgUp/PgDn · r refresh · Esc back"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "↑/↓ · r refresh · Esc back"
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
		available := max(1, height-2)
		start := min(max(0, state.offset), max(0, len(state.lines)-available))
		for _, line := range state.lines[start:min(len(state.lines), start+available)] {
			lines = append(lines, fixSurfaceLine(line, width, style.SurfaceModal, style.TextPrimary))
		}
	}
	if state.truncated {
		lines = append(lines, fixSurfaceLine("Output truncated · refresh or inspect retained transcript", width, style.SurfaceModal, style.TextMuted))
	}
	return fitFixContent(lines, width, height)
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
	footer := "↑/↓ choose · Enter · Esc continue editing"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "↑/↓ · Enter · Esc edit"
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

func (model Model) promptDetachView(base string) string {
	content := []string{"Form controls will no longer regenerate this task body.", "The Slopwatch safety envelope remains locked."}
	footer := "Enter detach+save · Esc continue editing"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "Enter detach · Esc edit"
	}
	if fullScreenSurface(model.width, model.height) {
		lines := []string{fixSurfaceLine("DETACH GENERATED TASK", model.width, style.SurfaceHeader, style.TextPrimary)}
		for _, line := range content {
			lines = append(lines, fixSurfaceLine(line, model.width, style.SurfaceModal, style.TextPrimary))
		}
		for len(lines) < model.height-1 {
			lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
		}
		lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
		return joinScreenLines(lines[:model.height])
	}
	return model.overlay(base, style.Popup("DETACH GENERATED TASK", content, footer, min(68, max(32, model.width-4))))
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
	return model.overlay(base, style.Popup("ACTIVE FIX JOBS", []string{status, "Running jobs do not detach from Slopwatch."}, footer, min(64, max(32, model.width-4))))
}

func (model Model) fixDialogPopup() string {
	width := min(76, max(36, model.width-4))
	contentHeight := max(4, min(12, model.height-7))
	content := model.fixDialogContent(width-4, contentHeight)
	return style.Popup("FIX FILE", content, model.fixDialogFooter(), width)
}

func (model Model) fixDialogFullScreen() string {
	lines := []string{fixSurfaceLine("FIX FILE", model.width, style.SurfaceHeader, style.TextPrimary)}
	lines = append(lines, model.fixDialogContent(model.width, max(1, model.height-2))...)
	for len(lines) < model.height-1 {
		lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine(model.fixDialogFooter(), model.width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:model.height])
}

func (model Model) fixDialogFooter() string {
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		switch model.fixDialog.cursor {
		case fixFieldFocus:
			return "↑/↓ · ←/→ metric · Space · Esc"
		case fixFieldBranch:
			return "↑/↓ · Enter branch · Esc"
		case fixFieldAdvanced:
			return "↑/↓ · Enter edit · P preview · Esc"
		case fixFieldRun:
			return "↑/↓ · Enter run · Esc"
		default:
			return "↑/↓ · ←/→ change · Esc"
		}
	}
	switch model.fixDialog.cursor {
	case fixFieldFocus:
		return "←/→ metric · Space toggle · P preview · ↑/↓ fields · Esc back"
	case fixFieldBranch:
		return "Enter edit branch · P preview · ↑/↓ fields · Esc back"
	case fixFieldAdvanced:
		return "Enter detach/edit · P preview · ↑/↓ fields · Esc back"
	case fixFieldRun:
		return "Enter run fix · P preview · ↑/↓ fields · Esc back"
	default:
		return "←/→ change · P preview · ↑/↓ fields · Esc back"
	}
}

func (model Model) fixDialogContent(width, height int) []string {
	state := model.fixDialog
	lines := []string{fixSurfaceLine("Target: "+state.target.String(), width, style.SurfaceModal, style.TextMuted)}
	if state.loading || !state.hasDraft {
		message := state.statusText
		if state.errorText != "" {
			message = "Error: " + state.errorText
		}
		lines = append(lines, fixSurfaceLine(message, width, style.SurfaceModal, style.TextPrimary))
		return fitFixContent(lines, width, height)
	}
	fields := model.fixFieldRows(width)
	available := max(1, height-len(lines)-2)
	start := min(max(0, state.cursor-available/2), max(0, len(fields)-available))
	end := min(len(fields), start+available)
	lines = append(lines, fields[start:end]...)
	status := state.statusText
	if state.errorText != "" {
		status = "Error: " + state.errorText
	} else if !fixDraftRunnable(state.draft) {
		status = "Run disabled: " + fixPreflightSummary(state.draft)
	}
	lines = append(lines, fixSurfaceLine(status, width, style.SurfaceModal, style.TextMuted))
	return fitFixContent(lines, width, height)
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
	metrics := "-"
	if len(state.metrics) > 0 {
		parts := make([]string, len(state.metrics))
		for index, id := range state.metrics {
			mark := " "
			if state.focus[id] {
				mark = "x"
			}
			label := strings.ToUpper(string(id))
			if index == state.metricCursor {
				label = "<" + label + ">"
			}
			parts[index] = "[" + mark + "] " + label
		}
		metrics = strings.Join(parts, "  ")
	}
	validation := state.draft.ValidationPlanID
	if validation == "" {
		validation = "none"
	}
	branch := state.branch.Value()
	if state.branchEditing {
		branch = state.branch.View()
	}
	delegation := string(state.draft.Delegation)
	if delegation == string(agent.DelegationSingle) {
		delegation = "single agent"
	}
	advanced := "edit generated task body"
	if state.detached {
		advanced = "DETACHED · edit task body"
	}
	values := []string{
		fmt.Sprintf("Target SCORE  ≤ %.0f", state.draft.TargetScore),
		"Focus metrics  " + metrics,
		"Agent profile  " + state.draft.Profile.Label,
		"Model          " + string(state.draft.Model),
		"Effort         " + string(state.draft.Effort),
		"Delegation     " + delegation,
		"Change scope   " + state.draft.ChangeScope,
		"Validation     " + validation,
		"Delivery       " + string(state.draft.DeliveryMode),
		"Branch         " + branch,
		"Advanced       " + advanced + " (safety envelope locked)",
		"Run fix",
	}
	rows := make([]string, len(values))
	for index, value := range values {
		prefix := "  "
		if index == state.cursor {
			prefix = "› "
		}
		background := style.SelectionSurface(index == state.cursor)
		foreground := style.TextPrimary
		if index == fixFieldRun && !fixDraftRunnable(state.draft) {
			foreground = style.TextMuted
		}
		rows[index] = fixSurfaceLine(prefix+value, width, background, foreground)
	}
	return rows
}

func fixDraftRunnable(draft fixapp.FixDraft) bool {
	return draft.Preflight.Clean && draft.Preflight.Supported && draft.Probe.State == agent.ProbeReady &&
		draft.Probe.Capabilities.Isolation.EligibleForMutation() &&
		fixOptionContains(draft.Probe.Capabilities.Models, draft.Model) &&
		fixOptionContains(draft.Probe.Capabilities.Efforts, draft.Effort) &&
		fixOptionContains(draft.Probe.Capabilities.Delegation, draft.Delegation) &&
		(draft.DeliveryMode == "candidate" || strings.TrimSpace(draft.BranchName) != "")
}

func fixOptionContains[T ~string](options []agent.Option[T], wanted T) bool {
	for _, option := range options {
		if option.ID == wanted {
			return true
		}
	}
	return false
}

func fixPreflightSummary(draft fixapp.FixDraft) string {
	if !draft.Preflight.Supported || !draft.Preflight.Clean {
		if draft.Preflight.Diagnostic != "" {
			return cleanAgentText(draft.Preflight.Diagnostic)
		}
		return "workspace preflight failed"
	}
	if draft.Probe.State != agent.ProbeReady {
		return fmt.Sprintf("agent is %s: %s", draft.Probe.State, cleanAgentText(draft.Probe.Diagnostic))
	}
	if !draft.Probe.Capabilities.Isolation.EligibleForMutation() {
		return "agent runtime isolation is not safe for mutation"
	}
	if !fixOptionContains(draft.Probe.Capabilities.Models, draft.Model) ||
		!fixOptionContains(draft.Probe.Capabilities.Efforts, draft.Effort) ||
		!fixOptionContains(draft.Probe.Capabilities.Delegation, draft.Delegation) {
		return "selected model, effort, or delegation is unavailable"
	}
	if draft.DeliveryMode != "candidate" && strings.TrimSpace(draft.BranchName) == "" {
		return "branch name is required for this delivery mode"
	}
	return "ready"
}

func (model Model) promptEditorView() string {
	width, height := model.width, model.height
	if width <= 0 || height <= 0 {
		return ""
	}
	editor := model.fixDialog.prompt
	editor.SetWidth(max(1, width))
	editor.SetHeight(max(1, height-2))
	title := "ADVANCED TASK BODY · safety envelope locked"
	footer := "Ctrl-S apply · Esc back"
	if model.fixDialog.promptPreview {
		title = "TASK BODY PREVIEW · safety envelope locked"
		footer = "e detach/edit · Esc back"
	} else if !model.fixDialog.detached {
		footer = "Ctrl-S detach+apply (confirmation required) · Esc back"
	}
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		if model.fixDialog.promptPreview {
			footer = "e detach/edit · Esc back"
		} else if model.fixDialog.detached {
			footer = "Ctrl-S apply · Esc back"
		} else {
			footer = "Ctrl-S detach · Esc back"
		}
	}
	lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
	for _, line := range strings.Split(editor.View(), "\n") {
		lines = append(lines, fixSurfaceLineANSI(line, width, style.SurfaceModal))
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
		footer = "Esc back · action unavailable"
	}
	return style.Popup("CONFIRM "+strings.ToUpper(jobActionLabel(state.action)), content, footer, min(68, max(32, model.width-4)))
}

func (model Model) confirmationFullScreen() string {
	lines := []string{
		fixSurfaceLine("CONFIRM "+strings.ToUpper(jobActionLabel(model.cancelConfirmation.action)), model.width, style.SurfaceHeader, style.TextPrimary),
		fixSurfaceLine("Job: "+string(model.cancelConfirmation.jobID), model.width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(jobActionConfirmationQuestion(model.cancelConfirmation.action), model.width, style.SurfaceModal, style.TextPrimary),
	}
	for _, value := range jobActionOutcomeLines(model.cancelConfirmation) {
		lines = append(lines, fixSurfaceLine(value, model.width, style.SurfaceModal, style.TextPrimary))
	}
	if model.cancelConfirmation.errorText != "" {
		lines = append(lines, fixSurfaceLine("Error: "+model.cancelConfirmation.errorText, model.width, style.SurfaceModal, style.TextPrimary))
	}
	for len(lines) < model.height-1 {
		lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
	}
	footer := "Enter confirm · Esc stay"
	if !model.cancelConfirmation.allowed {
		footer = "Esc back · action unavailable"
	}
	lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:model.height])
}

func (model Model) jobActionsPopup() string {
	width := min(64, max(36, model.width-4))
	content := model.jobActionsContent(width-4, max(3, min(12, model.height-7)))
	return style.Popup("JOB ACTIONS", content, "↑/↓ choose · Enter run · Esc back", width)
}

func (model Model) jobActionsFullScreen() string {
	lines := []string{fixSurfaceLine("JOB ACTIONS", model.width, style.SurfaceHeader, style.TextPrimary)}
	lines = append(lines, model.jobActionsContent(model.width, max(1, model.height-2))...)
	for len(lines) < model.height-1 {
		lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine("↑/↓ choose · Enter run · Esc back", model.width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:model.height])
}

func (model Model) jobActionsContent(width, height int) []string {
	state := model.jobActions
	lines := []string{fixSurfaceLine("Job: "+string(state.jobID), width, style.SurfaceModal, style.TextMuted)}
	if len(state.actions) == 0 {
		lines = append(lines, fixSurfaceLine("No actions are allowed in the current phase", width, style.SurfaceModal, style.TextMuted))
	} else {
		for index, action := range state.actions {
			prefix := "  "
			if index == state.cursor {
				prefix = "› "
			}
			label := jobActionLabel(action)
			if jobActionRequiresConfirmation(action) {
				label += " · confirms"
			}
			lines = append(lines, fixSurfaceLine(prefix+label, width, style.SelectionSurface(index == state.cursor), style.TextPrimary))
		}
	}
	status := "Only currently allowed actions are shown"
	if state.pending {
		status = "Applying action…"
	}
	if state.errorText != "" {
		status = "Error: " + state.errorText
	}
	lines = append(lines, fixSurfaceLine(status, width, style.SurfaceModal, style.TextMuted))
	return fitFixContent(lines, width, height)
}

func jobActionLabel(action fix.JobAction) string {
	switch action {
	case fix.ActionRetry:
		return "Retry"
	case fix.ActionResume:
		return "Resume"
	case fix.ActionPublish:
		return "Publish"
	case fix.ActionKeep:
		return "Keep candidate"
	case fix.ActionArchive:
		return "Archive"
	case fix.ActionDiscard:
		return "Discard candidate"
	case fix.ActionCleanup:
		return "Cleanup candidate"
	case fix.ActionCancel:
		return "Cancel"
	case fix.ActionAcknowledgeConflict:
		return "Acknowledge conflict"
	default:
		return cleanAgentText(string(action))
	}
}

func jobActionConfirmationQuestion(action fix.JobAction) string {
	switch action {
	case fix.ActionPublish:
		return "Publish this independently verified candidate?"
	case fix.ActionDiscard:
		return "Permanently discard this candidate and its uncommitted changes?"
	case fix.ActionCleanup:
		return "Permanently clean up this retained candidate?"
	case fix.ActionCancel:
		return "Cancel only this job?"
	default:
		return "Apply " + strings.ToLower(jobActionLabel(action)) + "?"
	}
}

func jobActionOutcomeLines(state cancelConfirmation) []string {
	if state.action != fix.ActionPublish {
		return nil
	}
	mode := cleanAgentText(string(state.deliveryMode))
	if mode == "" {
		mode = "configured delivery"
	}
	outcome := "Outcome: create an exact commit and branch"
	if strings.Contains(strings.ToLower(mode), "pull") || strings.Contains(strings.ToLower(mode), "pr") {
		outcome = "Outcome: draft pull request via an exact commit and branch"
	}
	lines := []string{"Delivery: " + mode, outcome}
	if state.branchName != "" {
		lines = append(lines, "Branch: "+cleanAgentText(state.branchName))
	}
	return lines
}

func fixSurfaceLine(text string, width int, background, foreground lipgloss.Color) string {
	return lipgloss.NewStyle().Background(background).Foreground(foreground).
		Render(padANSI(truncate(cleanAgentText(text), width), width))
}

func fixSurfaceLineANSI(text string, width int, background lipgloss.Color) string {
	return lipgloss.NewStyle().Background(background).Render(padANSI(truncateANSI(text, width), width))
}
