package follow

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (model Model) featureOverlayView(base string, frame OverlayFrame) string {
	switch frame.Kind {
	case OverlayFixForm:
		if fullScreenSurface(model.width, model.height) {
			return model.fixDialogFullScreen()
		}
		return model.overlay(base, model.fixDialogPopup())
	case OverlayConfigSettings:
		if fullScreenSurface(model.width, model.height) {
			return model.configSettingsFullScreen()
		}
		return model.overlay(base, model.configSettingsView())
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
	footer := "↑/↓ details · Esc background · Enter actions · [/] job · l logs · d diff · C cancel"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "↑/↓ details · Esc back · Enter actions"
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
		fixSurfaceLine("Agent: "+strings.Join(nonemptyStrings(strings.Join(nonemptyStrings(job.ProfileLabel, job.ModelLabel, job.EffortLabel), " / "), agentAttemptText(job)), " · "), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine("State: "+agentPhaseText(job)+" · "+nonemptySetting(job.CurrentAction, "No current activity"), width, style.SurfaceModal, style.TextPrimary),
		fixSurfaceLine(fmt.Sprintf("Targets: %d · Compliance: %s · Validation: %s · Scope: %s", len(job.Targets), job.Compliance, job.Validation, job.Scope), width, style.SurfaceModal, style.TextPrimary),
	}
	if job.UsageReported {
		lines = append(lines, fixSurfaceLine(fmt.Sprintf("Usage: in %d · cached %d · out %d · reasoning %d", job.Usage.InputTokens, job.Usage.CachedTokens, job.Usage.OutputTokens, job.Usage.ReasoningTokens), width, style.SurfaceModal, style.TextPrimary))
	} else {
		lines = append(lines, fixSurfaceLine("Usage: not reported by this agent adapter", width, style.SurfaceModal, style.TextMuted))
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
	lines = append(lines, fixSurfaceLine("ACTIVITY", width, style.SurfaceModal, style.TextMuted))
	activity := state.activity
	if len(activity) == 0 {
		lines = append(lines, fixSurfaceLine("No provider activity recorded yet", width, style.SurfaceModal, style.TextMuted))
	} else {
		for _, entry := range activity {
			actor := ""
			if entry.ActorID != "" {
				actor = "[" + cleanAgentText(entry.ActorID) + "] "
			}
			lines = append(lines, fixSurfaceLine(entry.At.Format("15:04:05")+"  "+actor+cleanAgentText(entry.Summary), width, style.SurfaceModal, style.TextPrimary))
		}
	}
	if state.activityTruncated {
		lines = append(lines, fixSurfaceLine("Earlier activity omitted by the configured transcript retention limit", width, style.SurfaceModal, style.TextMuted))
	}
	if state.errorText != "" {
		lines = append(lines, fixSurfaceLine("Activity warning: "+state.errorText, width, style.SurfaceModal, style.TextPrimary))
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
	count++ // activity heading
	count += max(1, len(state.activity))
	if state.activityTruncated {
		count++
	}
	if state.errorText != "" {
		count++
	}
	return count
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
		message := "Output truncated"
		switch state.kind {
		case OverlayJobLog:
			message = "Earlier activity omitted by the configured transcript retention limit"
		case OverlayJobDiff:
			message = "More changed files exist · press r to retry loading them"
		case OverlayCandidateSource:
			message = "Preview truncated at the configured candidate byte/line limit"
		}
		lines = append(lines, fixSurfaceLine(message, width, style.SurfaceModal, style.TextMuted))
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
	if !model.fixDialog.loading && !model.fixDialogRunnable() {
		_, hasSettings := model.fixRemediationSettingsKind()
		if model.fixDialog.cursor == fixFieldValidation {
			if responsiveTier(model.width, model.height) == ResponsiveCompact {
				return "←/→ plan · r recheck · s Settings · Esc"
			}
			return "←/→ choose validation plan · r recheck readiness · s remediation settings · Esc back"
		}
		if responsiveTier(model.width, model.height) == ResponsiveCompact {
			if hasSettings {
				return "r recheck · s Settings · Esc"
			}
			return "r recheck · Esc back"
		}
		if hasSettings {
			return "r recheck readiness · s open remediation settings · Esc back"
		}
		return "Fix cannot run safely · r recheck · Esc back"
	}
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
		lines = append(lines, fixWrappedLines(message, width, max(1, height-len(lines)), style.TextPrimary)...)
		return fitFixContent(lines, width, height)
	}
	status := state.statusText
	if state.errorText != "" {
		status = "Error: " + state.errorText
	} else if state.deliveryStale {
		status = "Run disabled: delivery or branch changed · press r to recheck workspace and delivery readiness"
	} else if !model.fixDialogRunnable() {
		status = "Run disabled: " + fixPreflightSummary(state.draft)
	} else {
		status = "Ready · " + fixRuntimeCapabilitySummary(state.draft)
	}
	statusLines := fixWrappedLines(status, width, min(3, max(1, height-len(lines)-1)), style.TextMuted)
	fields := model.fixFieldRows(width)
	available := max(1, height-len(lines)-len(statusLines))
	start := min(max(0, state.cursor-available/2), max(0, len(fields)-available))
	end := min(len(fields), start+available)
	lines = append(lines, fields[start:end]...)
	lines = append(lines, statusLines...)
	return fitFixContent(lines, width, height)
}

func fixRuntimeCapabilitySummary(draft fixapp.FixDraft) string {
	isolation := draft.Probe.Capabilities.Isolation
	confinement := "Confinement: NOT PROVEN"
	if isolation.ProviderManagedCancellation && isolation.Writes >= agent.CandidateTreeEnforced {
		confinement = "Confinement: provider workspace sandbox with per-job cancellation"
	} else if isolation.EligibleForMutation() {
		confinement = "Confinement: enforced for candidate/Git, reads, auth, and child processes"
	}
	network := "Network: tools offline"
	if draft.Probe.Capabilities.Network.ToolNetwork {
		network = "Network: agent tools enabled"
		if domains := draft.Probe.Capabilities.Network.ToolDomains; len(domains) > 0 {
			network += " for " + strings.Join(domains, ",")
		}
	} else if draft.Probe.Capabilities.Network.TransportRequired {
		network = "Network: provider transport only; agent tools offline"
	}
	return confinement + " · " + network
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
	} else if !fixValidationRunnable(state.draft) {
		validation += " · NOT RUNNABLE"
	} else {
		validation += " · ready"
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
	profile := state.draft.Profile.Label
	if state.draft.Probe.Authentication.Label != "" {
		profile += " · " + state.draft.Probe.Authentication.Label
	}
	values := []string{
		fmt.Sprintf("Target SCORE  ≤ %.0f", state.draft.TargetScore),
		"Focus metrics  " + metrics,
		"Agent profile  " + profile,
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
		if index == fixFieldRun && !model.fixDialogRunnable() {
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
		(draft.DeliveryMode != "pull-request" || !draft.Preferences.Delivery.RequireValidation || strings.TrimSpace(draft.ValidationPlanID) != "") &&
		fixValidationRunnable(draft) &&
		(draft.DeliveryMode == "candidate" || strings.TrimSpace(draft.BranchName) != "")
}

func fixValidationPlanAvailable(draft fixapp.FixDraft) bool {
	if draft.ValidationPlanID == "" {
		return true
	}
	for _, plan := range draft.Preferences.Validation {
		if plan.ID == draft.ValidationPlanID && len(plan.Checks) > 0 {
			return true
		}
	}
	return false
}

func fixValidationRunnable(draft fixapp.FixDraft) bool {
	if draft.ValidationPlanID == "" {
		return !draft.ValidationReadiness.Required
	}
	return fixValidationPlanAvailable(draft) && draft.ValidationReadiness.Required && draft.ValidationReadiness.Ready
}

func fixValidationNeedsRepair(draft fixapp.FixDraft) bool {
	return (draft.DeliveryMode == fix.DeliveryModePullRequest && draft.Preferences.Delivery.RequireValidation && strings.TrimSpace(draft.ValidationPlanID) == "") || !fixValidationRunnable(draft)
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
	diagnostic := cleanAgentText(draft.Probe.Diagnostic)
	suffix := ""
	if diagnostic != "" {
		suffix = " · " + diagnostic
	}
	switch draft.Probe.State {
	case agent.ProbeUnauthenticated:
		return "agent is unauthenticated — open Settings › Agents and select it for connection guidance" + suffix
	case agent.ProbeUnavailable, agent.ProbeIncompatible:
		return fmt.Sprintf("agent is %s — open Settings › Agents and select it for connection guidance%s", draft.Probe.State, suffix)
	case agent.ProbeDegraded:
		if !draft.Probe.Capabilities.Isolation.EligibleForMutation() {
			return "runtime confinement is unsupported; this build cannot run fixes safely and Settings cannot enable it" + suffix
		}
		return "agent is degraded and cannot run fixes" + suffix
	default:
		if draft.Probe.State != agent.ProbeReady {
			return fmt.Sprintf("agent is %s and cannot run fixes%s", draft.Probe.State, suffix)
		}
	}
	if !draft.Probe.Capabilities.Isolation.EligibleForMutation() {
		return "runtime confinement is unsupported; this build cannot run fixes safely and Settings cannot enable it"
	}
	if !fixOptionContains(draft.Probe.Capabilities.Models, draft.Model) ||
		!fixOptionContains(draft.Probe.Capabilities.Efforts, draft.Effort) ||
		!fixOptionContains(draft.Probe.Capabilities.Delegation, draft.Delegation) {
		return "selected model, effort, or delegation is unavailable — repair Settings › Fix defaults"
	}
	if draft.DeliveryMode != "candidate" && strings.TrimSpace(draft.BranchName) == "" {
		return "branch name is required — repair Settings › Git & pull requests"
	}
	if draft.DeliveryMode == "pull-request" && draft.Preferences.Delivery.RequireValidation && strings.TrimSpace(draft.ValidationPlanID) == "" {
		if len(draft.Preferences.Validation) == 0 {
			return "pull-request delivery is configured to require validation, but no trusted plans are configured · add one in preferences, then press r"
		}
		return "pull-request delivery requires a ready validation plan — select one in Settings › Validation"
	}
	if !fixValidationPlanAvailable(draft) {
		for _, plan := range draft.Preferences.Validation {
			if plan.ID == draft.ValidationPlanID && len(plan.Checks) == 0 {
				return fmt.Sprintf("validation plan %q is unavailable: it has no trusted checks · repair the installation-owned validation configuration, then press r", draft.ValidationPlanID)
			}
		}
		if len(draft.Preferences.Validation) == 0 {
			return fmt.Sprintf("validation plan %q is unavailable because no installation-owned validation plans are configured · add a trusted plan in installation preferences, then press r", draft.ValidationPlanID)
		}
		return fmt.Sprintf("validation plan %q is unavailable — select a configured plan in Settings › Validation", draft.ValidationPlanID)
	}
	if !fixValidationRunnable(draft) {
		diagnostic := cleanAgentText(draft.ValidationReadiness.Diagnostic)
		if diagnostic == "" {
			diagnostic = "validation confinement or executable readiness was not proven"
		}
		return fmt.Sprintf("validation plan %q is NOT RUNNABLE · %s · choose a ready plan or none", draft.ValidationPlanID, diagnostic)
	}
	return "ready"
}

func (model Model) promptEditorView() string {
	width, height := model.width, model.height
	if width <= 0 || height <= 0 {
		return ""
	}
	editor := model.fixDialog.prompt
	editorValue := editor.Value()
	if model.fixDialog.promptPreview {
		document := model.fixDialog.draft.Instructions
		if model.fixDialog.detached {
			document.DetachedBody = editorValue
		}
		editorValue = document.EffectiveBody()
	}
	editor.SetValue(cleanEditorText(editorValue))
	editor.SetWidth(max(1, width))
	editor.SetHeight(max(1, height-2))
	title := "ADVANCED TASK BODY · safety envelope locked"
	footer := "Ctrl-S apply · Esc back"
	if model.fixDialog.promptPreview {
		title = "EFFECTIVE PROMPT PREVIEW · locked envelope included"
		footer = "e detach/edit · Esc back"
	} else if !model.fixDialog.detached {
		footer = "Ctrl-S detach+apply (confirmation required) · Esc back"
	} else if model.fixDialog.promptResetPending {
		footer = "R again reset from controls · Ctrl-S apply · Esc back"
	} else {
		footer = "Ctrl-S apply · R reset from controls · Esc back"
	}
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		if model.fixDialog.promptPreview {
			footer = "e detach/edit · Esc back"
		} else if model.fixDialog.detached {
			if model.fixDialog.promptResetPending {
				footer = "R again reset · Esc back"
			} else {
				footer = "Ctrl-S apply · R reset · Esc back"
			}
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
		outcome = "Outcome: create a pull request via an exact commit and branch"
	}
	lines := []string{"Delivery: " + mode}
	if state.branchName != "" {
		lines = append(lines, "Branch: "+cleanAgentText(state.branchName))
	}
	lines = append(lines, outcome)
	return lines
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
