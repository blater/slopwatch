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
	case OverlayJobDiff:
		title = "CANDIDATE DIFF"
	case OverlayCandidateSource:
		title = "CANDIDATE SOURCE"
	}
	width := min(92, max(36, model.width-4))
	height := max(4, min(20, model.height-7))
	content := model.jobReaderContent(width-4, height)
	footer := "PgUp/PgDn · r refresh · Esc back"
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "r refresh · Esc back"
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
	if model.fixDialog.submitting {
		return "Starting fix…"
	}
	if model.fixDialog.loading {
		return "Checking readiness… · Esc back"
	}
	if !model.fixDialog.loading && !model.fixDialogRunnable() {
		_, hasSettings := model.fixRemediationSettingsKind()
		if model.fixDialog.cursor == fixFieldValidation {
			if responsiveTier(model.width, model.height) == ResponsiveCompact {
				return "←/→ plan · R recheck · s Settings · Esc"
			}
			return "←/→ choose validation plan · R recheck readiness · s remediation settings · Esc back"
		}
		if responsiveTier(model.width, model.height) == ResponsiveCompact {
			if hasSettings {
				return "R recheck · s Settings · Esc"
			}
			return "R recheck · Esc back"
		}
		if hasSettings {
			return "R recheck readiness · s open remediation settings · Esc back"
		}
		return "Fix cannot run safely · R recheck · Esc back"
	}
	if responsiveTier(model.width, model.height) == ResponsiveCompact {
		switch model.fixDialog.cursor {
		case fixFieldFocus:
			return "←/→ metric · Space · r run · Esc"
		case fixFieldBranch:
			return "Enter branch · r run · Esc"
		default:
			return "←/→ change · r run · Esc"
		}
	}
	switch model.fixDialog.cursor {
	case fixFieldFocus:
		return "←/→ metric · Space toggle · r run · Esc back"
	case fixFieldBranch:
		return "Enter edit branch · r run · Esc back"
	default:
		return "←/→ change · r run · Esc back"
	}
}

func (model Model) fixDialogContent(width, height int) []string {
	state := model.fixDialog
	lines := []string{fixSurfaceLine("Target: "+state.target.String(), width, style.SurfaceModal, style.TextMuted)}
	if state.loading || !state.hasDraft {
		message := "CHECKING READINESS…"
		if state.errorText != "" {
			message = "Error: " + state.errorText
		}
		lines = append(lines, fixWrappedLines(message, width, max(1, height-len(lines)), style.TextPrimary)...)
		return fitFixContent(lines, width, height)
	}
	status := state.statusText
	if state.submitting {
		status = "STARTING FIX…"
	} else if state.errorText != "" {
		status = "Error: " + state.errorText
	} else if state.deliveryStale {
		status = "RECHECK REQUIRED · press R"
	} else if !model.fixDialogRunnable() {
		status = "FIX BLOCKED · " + fixPreflightSummary(state.draft)
	} else {
		status = "READY TO FIX"
	}
	statusLines := fixWrappedLines(status, width, min(3, max(1, height-len(lines)-1)), style.TextMuted)
	fields := model.fixFieldRows(width)
	available := max(1, height-len(lines)-len(statusLines))
	cursor := fixFieldPosition(model.fixVisibleFields(), state.cursor)
	start := min(max(0, cursor-available/2), max(0, len(fields)-available))
	end := min(len(fields), start+available)
	lines = append(lines, fields[start:end]...)
	lines = append(lines, statusLines...)
	return fitFixContent(lines, width, height)
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
	selectedMetrics := 0
	for _, selected := range state.focus {
		if selected {
			selectedMetrics++
		}
	}
	metric := ""
	if len(state.metrics) > 0 {
		index := min(max(0, state.metricCursor), len(state.metrics)-1)
		id := state.metrics[index]
		mark := " "
		if state.focus[id] {
			mark = "x"
		}
		metric = fmt.Sprintf("Metric %d/%d     [%s] %s · %d selected", index+1, len(state.metrics), mark, fixMetricLabel(id), selectedMetrics)
	}
	values := map[int]string{
		fixFieldTargetScore: fmt.Sprintf("Target score    ≤ %.0f", state.draft.TargetScore),
		fixFieldFocus:       metric,
		fixFieldProfile:     "Agent           " + model.fixAgentLabel(),
		fixFieldModel:       "Model           " + agentOptionLabel(state.draft.Probe.Capabilities.Models, state.draft.Model),
		fixFieldEffort:      "Effort          " + agentOptionLabel(state.draft.Probe.Capabilities.Efforts, state.draft.Effort),
		fixFieldDelegation:  "Use agent team  " + delegationLabel(state.draft.Delegation),
		fixFieldScope:       "May edit        " + changeScopeLabel(state.draft.ChangeScope),
		fixFieldValidation:  "Validation      " + validation,
		fixFieldDelivery:    "Delivery        " + deliveryModeLabel(state.draft.DeliveryMode),
		fixFieldBranch:      "Branch name     " + branch,
	}
	fields := model.fixVisibleFields()
	rows := make([]string, len(fields))
	for index, field := range fields {
		value := values[field]
		prefix := "  "
		if field == state.cursor {
			prefix = "› "
		}
		background := style.SelectionSurface(field == state.cursor)
		rows[index] = fixSurfaceLine(prefix+value, width, background, style.TextPrimary)
	}
	return rows
}

func (model Model) fixVisibleFields() []int {
	state := model.fixDialog
	fields := []int{fixFieldTargetScore}
	if len(state.metrics) > 0 {
		fields = append(fields, fixFieldFocus)
	}
	fields = append(fields, fixFieldProfile, fixFieldModel, fixFieldEffort)
	if len(state.draft.Probe.Capabilities.Delegation) > 1 {
		fields = append(fields, fixFieldDelegation)
	}
	fields = append(fields, fixFieldScope)
	if len(state.draft.Preferences.Validation) > 0 {
		fields = append(fields, fixFieldValidation)
	}
	fields = append(fields, fixFieldDelivery)
	fields = append(fields, fixFieldBranch)
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
	profile := model.fixDialog.draft.Profile
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
	for _, option := range options {
		if option.ID == selected {
			return nonemptySetting(cleanAgentText(option.Label), string(option.ID))
		}
	}
	return string(selected)
}

func delegationLabel(value agent.DelegationMode) string {
	if value == agent.DelegationSingle {
		return "No"
	}
	return "Yes"
}

func changeScopeLabel(value string) string {
	switch value {
	case "targets-only":
		return "Selected file only"
	case "repository":
		return "Any file in repository"
	default:
		return "Selected file + related tests"
	}
}

func deliveryModeLabel(value fix.DeliveryMode) string {
	switch value {
	case fix.DeliveryModeBranch:
		return "Push branch"
	case fix.DeliveryModePullRequest:
		return "Open pull request"
	default:
		return "Apply changes"
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

func fixDraftRunnable(draft fixapp.FixDraft) bool {
	return draft.Preflight.Ready && draft.Preflight.Supported && draft.Probe.State == agent.ProbeReady &&
		draft.Probe.Capabilities.Isolation.EligibleForMutation() &&
		fixOptionContains(draft.Probe.Capabilities.Models, draft.Model) &&
		fixOptionContains(draft.Probe.Capabilities.Efforts, draft.Effort) &&
		fixOptionContains(draft.Probe.Capabilities.Delegation, draft.Delegation) &&
		(draft.DeliveryMode != "pull-request" || !draft.Preferences.Delivery.RequireValidation || strings.TrimSpace(draft.ValidationPlanID) != "") &&
		fixValidationRunnable(draft) &&
		strings.TrimSpace(draft.BranchName) != ""
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
	if !draft.Preflight.Supported || !draft.Preflight.Ready {
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

func (model Model) masterPromptEditorView() string {
	width, height := model.width, model.height
	if width <= 0 || height <= 0 {
		return ""
	}
	editor := model.configSettings.prompt
	editorValue := editor.Value()
	editor.SetValue(cleanEditorText(editorValue))
	editor.SetWidth(max(1, width))
	errorText := cleanAgentText(model.configSettings.promptError)
	errorRows := 0
	if errorText != "" {
		errorRows = 1
	}
	editor.SetHeight(max(1, height-2-errorRows))
	title := "MASTER AGENT PROMPT"
	footer := "Ctrl-S apply · Esc back"
	lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
	for _, line := range strings.Split(editor.View(), "\n") {
		lines = append(lines, fixSurfaceLineANSI(line, width, style.SurfaceModal))
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
