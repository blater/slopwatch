package follow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

// FixService is the narrow application boundary consumed by follow mode. It
// deliberately exposes projections and commands, never cache, provider, Git,
// preference-file, or authorization internals.
type FixService interface {
	Prepare(context.Context, fixapp.PrepareRequest) (fixapp.FixDraft, error)
	Submit(context.Context, fixapp.SubmitRequest) (fix.JobID, error)
	Jobs(fixapp.JobFilter) fixapp.JobListSnapshot
	Job(fix.JobID) (fix.JobPresentation, bool)
	Subscribe() fixapp.Subscription
	Execute(context.Context, fix.JobCommand) (fixapp.CommandReceipt, error)
	CandidateFile(context.Context, fix.JobID, fix.RepoPath) (candidate.File, error)
	Diff(context.Context, fix.JobID, fixapp.DiffRequest) (fixapp.DiffPage, error)
	Transcript(context.Context, fix.JobID, fixapp.LogCursor, int) (fixapp.LogPage, error)
	Reconfigure(context.Context, fixapp.RuntimeLimits) error
	Shutdown(context.Context) error
}

const (
	fixFieldTargetScore = iota
	fixFieldFocus
	fixFieldProfile
	fixFieldModel
	fixFieldEffort
	fixFieldDelegation
	fixFieldScope
	fixFieldValidation
	fixFieldDelivery
	fixFieldBranch
	fixFieldCount
)

type fixDialogState struct {
	generation     uint64
	target         fix.RepoPath
	draft          fixapp.FixDraft
	hasDraft       bool
	loading        bool
	submitting     bool
	cursor         int
	metricCursor   int
	focus          map[fix.MetricID]bool
	metrics        []fix.MetricID
	branch         textinput.Model
	branchOriginal string
	branchEditing  bool
	deliveryStale  bool
	submitBlocked  bool
	errorText      string
	statusText     string
}

type cancelConfirmation struct {
	jobID        fix.JobID
	revision     uint64
	action       fix.JobAction
	deliveryMode fix.DeliveryMode
	branchName   string
	diffHash     string
	allowed      bool
	pending      bool
	errorText    string
}

type jobCommandState struct {
	jobID    fix.JobID
	revision uint64
	action   fix.JobAction
	pending  bool
}

type jobMonitorState struct {
	generation        uint64
	jobID             fix.JobID
	focusPath         fix.RepoPath
	job               fix.JobPresentation
	activity          []fixapp.LogEntry
	activityTruncated bool
	loading           bool
	refreshing        bool
	pending           bool
	errorText         string
	offset            int
}

type jobReaderState struct {
	generation uint64
	kind       OverlayKind
	jobID      fix.JobID
	path       fix.RepoPath
	lines      []string
	loading    bool
	truncated  bool
	errorText  string
	offset     int
}

type shutdownState struct {
	active    int
	pending   bool
	errorText string
}

type fixPreparedMsg struct {
	generation uint64
	draft      fixapp.FixDraft
	err        error
}

type fixSubmittedMsg struct {
	generation uint64
	jobID      fix.JobID
	err        error
}

type fixTargetPreferenceSavedMsg struct {
	score float64
	saved appconfig.Saved
	err   error
}

type fixJobsMsg struct {
	revision fixapp.GlobalRevision
	jobs     []fix.JobPresentation
	err      error
}

type fixCommandMsg struct {
	jobID   fix.JobID
	action  fix.JobAction
	receipt fixapp.CommandReceipt
	err     error
}

type fixRetrySubscriptionMsg struct{ generation uint64 }

type jobMonitorMsg struct {
	generation uint64
	job        fix.JobPresentation
	activity   fixapp.LogPage
	found      bool
	err        error
}

type jobReaderMsg struct {
	generation uint64
	kind       OverlayKind
	lines      []string
	truncated  bool
	err        error
}

type shutdownCompleteMsg struct{ err error }

func (model *Model) openFixForSelected() tea.Cmd {
	path, err := model.selectedRepoPath(model.selected)
	if err != nil {
		model.status = "Fix unavailable for this row: " + err.Error()
		return nil
	}
	if job, ok := model.existingFixForTarget(path); ok {
		model.switchMainView(MainViewAgents)
		model.agents.FindQuery = ""
		model.agents.Selected = AgentRowID{JobID: job.ID}
		model.ensureAgentVisible()
		model.fixNotice = "Opened existing fix for " + path.String()
		return nil
	}
	if model.fixService == nil {
		model.status = "Fix unavailable: configure an agent service in Settings"
		if model.options.FixUnavailableReason != "" {
			model.status = "Fix unavailable: " + model.options.FixUnavailableReason
		}
		return nil
	}
	model.fixGeneration++
	branch := textinput.New()
	branch.Prompt = ""
	model.fixDialog = fixDialogState{
		generation: model.fixGeneration, target: path, loading: true, focus: map[fix.MetricID]bool{}, branch: branch,
		statusText: "Preparing baseline and checking agent readiness…",
	}
	model.overlays.Push(OverlayFixForm, OverlayCaller{MainView: MainViewFiles, Selected: model.selected})
	return model.prepareFixCommand(path, nil, model.fixGeneration)
}

func (model Model) selectedRepoPath(value string) (fix.RepoPath, error) {
	if model.fixWorkspace.RepositoryRoot == "" {
		return fix.ParseRepoPath(filepath.ToSlash(value))
	}
	absolute := value
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(model.options.Workspace, value)
	}
	absolute, err := filepath.Abs(absolute)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(model.fixWorkspace.RepositoryRoot, absolute)
	if err != nil {
		return "", err
	}
	return fix.ParseRepoPath(filepath.ToSlash(relative))
}

func (model Model) existingFixForTarget(path fix.RepoPath) (fix.JobPresentation, bool) {
	candidates := make([]fix.JobPresentation, 0, 2)
	for _, job := range model.agents.Jobs {
		if job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseCanceled || job.Phase == fix.PhaseDiscarded {
			continue
		}
		if job.Issue != nil && job.Issue.Code == "canceled" && (job.Phase == fix.PhaseCanceling || job.Phase == fix.PhaseDiscarding) {
			continue
		}
		for _, target := range job.Targets {
			if target.Path == path {
				candidates = append(candidates, job)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return fix.JobPresentation{}, false
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		leftPriority, rightPriority := agentJobPriority(candidates[left]), agentJobPriority(candidates[right])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return candidates[left].UpdatedAt.After(candidates[right].UpdatedAt)
	})
	return candidates[0], true
}

func (model Model) fixMarkerForPath(path string) string {
	if len(model.agents.Jobs) == 0 {
		return ""
	}
	repoPath, err := model.selectedRepoPath(path)
	if err != nil {
		return ""
	}
	var selected fix.JobPresentation
	found := false
	for _, job := range model.agents.Jobs {
		for _, target := range job.Targets {
			if target.Path != repoPath {
				continue
			}
			if !found || agentJobPriority(job) < agentJobPriority(selected) ||
				agentJobPriority(job) == agentJobPriority(selected) && job.UpdatedAt.After(selected.UpdatedAt) {
				selected, found = job, true
			}
		}
	}
	if !found {
		return ""
	}
	if selected.Phase == fix.PhaseFailed {
		if selected.Issue != nil && strings.EqualFold(selected.Issue.Code, "canceled") {
			return "×"
		}
		return "!"
	}
	switch selected.Phase {
	case fix.PhaseAdmitted, fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing:
		return "…"
	case fix.PhaseRunning:
		return "▶"
	case fix.PhaseWaitingVerifier:
		return "◷"
	case fix.PhaseVerifying:
		return "◆"
	case fix.PhaseFailed:
		return "!"
	case fix.PhasePublishing:
		return "↑"
	case fix.PhaseCanceling:
		return "×"
	case fix.PhaseCanceled:
		return "×"
	case fix.PhaseReconciling:
		return "↻"
	case fix.PhaseCompleted:
		return "✓"
	case fix.PhaseDiscarding, fix.PhaseDiscarded:
		return "×"
	default:
		return ""
	}
}

func (model Model) prepareFixCommand(path fix.RepoPath, profile *agent.ProfileID, generation uint64) tea.Cmd {
	service := model.fixService
	workspace := model.fixWorkspace
	var selectedDelivery *fixapp.PrepareDelivery
	if model.fixDialog.hasDraft {
		selectedDelivery = &fixapp.PrepareDelivery{Mode: model.fixDialog.draft.DeliveryMode, Branch: model.fixDialog.draft.BranchName}
	}
	if workspace.RepositoryRoot == "" {
		workspace.RepositoryRoot = model.options.Workspace
		workspace.AnalysisRoot = model.options.Workspace
	}
	return func() tea.Msg {
		overrides := appconfig.SessionOverrides{Profile: profile}
		draft, err := service.Prepare(context.Background(), fixapp.PrepareRequest{
			Workspace: workspace, Targets: []fix.RepoPath{path}, Overrides: overrides, Delivery: selectedDelivery,
		})
		return fixPreparedMsg{generation: generation, draft: draft, err: err}
	}
}

func (model *Model) handleFixPrepared(message fixPreparedMsg) {
	if message.generation != model.fixDialog.generation || !model.hasOverlay(OverlayFixForm) {
		return
	}
	model.fixDialog.loading = false
	if message.err != nil {
		model.fixDialog.errorText = message.err.Error()
		model.fixDialog.statusText = "Preparation failed"
		return
	}
	old := model.fixDialog
	model.fixDialog.draft = message.draft
	model.fixDialog.hasDraft = true
	model.fixDialog.submitBlocked = false
	model.fixDialog.deliveryStale = false
	model.fixDialog.errorText = ""
	model.fixDialog.statusText = "Ready"
	model.fixDialog.metrics = availableFixMetrics(message.draft)
	model.fixDialog.focus = map[fix.MetricID]bool{"score": true}
	for _, goal := range message.draft.Focus {
		model.fixDialog.focus[goal.Metric] = true
	}
	if old.hasDraft {
		if old.draft.Model != message.draft.Model || old.draft.Effort != message.draft.Effort || old.draft.Delegation != message.draft.Delegation {
			model.fixDialog.statusText = fmt.Sprintf("Profile changed · using %s / %s / %s", message.draft.Model, message.draft.Effort, message.draft.Delegation)
		}
		edits := old.fixDraftEdits()
		if revised, err := fixapp.ReviseDraft(message.draft, edits); err == nil {
			model.fixDialog.draft = revised
		}
		model.fixDialog.focus = old.focus
		model.fixDialog.branch.SetValue(old.branch.Value())
		model.fixDialog.branchOriginal = old.branchOriginal
	} else {
		model.fixDialog.branch.SetValue(message.draft.BranchName)
		model.fixDialog.branchOriginal = message.draft.BranchName
	}
	if fixValidationNeedsRepair(model.fixDialog.draft) {
		if len(model.fixDialog.draft.Preferences.Validation) > 0 {
			model.fixDialog.cursor = fixFieldValidation
		}
	}
	model.ensureFixCursorVisible()
}

func availableFixMetrics(draft fixapp.FixDraft) []fix.MetricID {
	seen := map[fix.MetricID]bool{}
	for _, target := range draft.Baseline.Contract.Targets {
		for id, metric := range target.Metrics {
			if metric.Complete {
				seen[id] = true
			}
		}
	}
	result := make([]fix.MetricID, 0, len(seen)+1)
	for id := range seen {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return append([]fix.MetricID{"score"}, result...)
}

func (state fixDialogState) fixDraftEdits() fixapp.DraftEdits {
	goals := make([]fix.MetricGoal, 0, len(state.focus))
	for _, id := range state.metrics {
		if !state.focus[id] {
			continue
		}
		if id == "score" {
			continue
		}
		maximum := 0.0
		found := false
		for _, target := range state.draft.Baseline.Contract.Targets {
			if metric, ok := target.Metrics[id]; ok && metric.Complete && (!found || metric.Value > maximum) {
				maximum, found = metric.Value, true
			}
		}
		if found {
			goals = append(goals, fix.MetricGoal{Metric: id, Maximum: maximum})
		}
	}
	return fixapp.DraftEdits{
		TargetScore: state.draft.TargetScore,
		Focus:       goals, ChangeScope: state.draft.ChangeScope,
		ValidationPlanID: state.draft.ValidationPlanID, DeliveryMode: state.draft.DeliveryMode,
		BranchName: state.branch.Value(),
	}
}

func (model *Model) syncFixDraft() bool {
	revised, err := fixapp.ReviseDraft(model.fixDialog.draft, model.fixDialog.fixDraftEdits())
	if err != nil {
		model.fixDialog.errorText = err.Error()
		return false
	}
	model.fixDialog.draft = revised
	model.fixDialog.errorText = ""
	return true
}

func (model *Model) handleFixFormKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	state := &model.fixDialog
	if state.branchEditing {
		switch name {
		case "enter":
			state.branchEditing = false
			state.branch.Blur()
			model.syncFixDraft()
			if state.branch.Value() != state.branchOriginal {
				model.markFixDeliveryStale()
			}
			state.branchOriginal = state.branch.Value()
			return model, nil
		case "esc":
			state.branchEditing = false
			state.branch.Blur()
			state.branch.SetValue(state.branchOriginal)
			return model, nil
		default:
			updated, command := state.branch.Update(key)
			state.branch = updated
			return model, command
		}
	}
	if state.submitting && (name == "esc" || name == "q") {
		state.statusText = "Submission in progress…"
		return model, nil
	}
	if name == "esc" || name == "q" {
		model.fixDialog.generation++
		model.overlays.Pop()
		return model, nil
	}
	if state.loading || state.submitting || !state.hasDraft {
		if name == "R" && !state.loading && !state.submitting {
			model.fixGeneration++
			state.generation = model.fixGeneration
			state.loading = true
			state.errorText = ""
			state.statusText = "Rechecking runtime, validation, and workspace readiness…"
			return model, model.prepareFixCommand(state.target, nil, state.generation)
		}
		if name == "s" && !state.submitting && state.errorText != "" {
			return model, model.openFixRemediationSettings()
		}
		return model, nil
	}
	switch name {
	case "r":
		return model.submitFix()
	case "R":
		model.fixGeneration++
		state.generation = model.fixGeneration
		state.loading = true
		state.submitBlocked = false
		state.errorText = ""
		state.statusText = "Rechecking runtime, validation, and workspace readiness…"
		profile := state.draft.Profile.ID
		return model, model.prepareFixCommand(state.target, &profile, state.generation)
	case "s":
		return model, model.openFixRemediationSettings()
	case "up", "k":
		model.moveFixCursor(-1)
	case "down", "j":
		model.moveFixCursor(1)
	case "left":
		return model.adjustFixField(-1)
	case "right":
		return model.adjustFixField(1)
	case " ":
		if state.cursor == fixFieldFocus && len(state.metrics) > 0 {
			id := state.metrics[state.metricCursor]
			if id != "score" {
				state.focus[id] = !state.focus[id]
				model.syncFixDraft()
			}
		}
	case "enter":
		if state.cursor == fixFieldFocus && len(state.metrics) > 0 {
			id := state.metrics[state.metricCursor]
			if id != "score" {
				state.focus[id] = !state.focus[id]
				model.syncFixDraft()
			}
			return model, nil
		}
		switch state.cursor {
		case fixFieldBranch:
			state.branchOriginal = state.branch.Value()
			state.branchEditing = true
			state.branch.Focus()
		}
	}
	return model, nil
}

func (model *Model) adjustFixField(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch state.cursor {
	case fixFieldTargetScore:
		state.draft.TargetScore = max(0, state.draft.TargetScore+float64(direction*10))
		if model.syncFixDraft() {
			model.fixTargetDesired = state.draft.TargetScore
			if !model.fixTargetSaving {
				return model, model.saveFixTargetPreference(state.draft.Preferences)
			}
		}
	case fixFieldFocus:
		if len(state.metrics) > 0 {
			state.metricCursor = fixCycleIndex(state.metricCursor, direction, len(state.metrics))
		}
	case fixFieldProfile:
		profiles := state.draft.Preferences.Profiles
		if len(profiles) > 1 {
			current := 0
			for index := range profiles {
				if profiles[index].ID == state.draft.Profile.ID {
					current = index
				}
			}
			profile := profiles[fixCycleIndex(current, direction, len(profiles))].ID
			model.fixGeneration++
			state.generation = model.fixGeneration
			state.loading = true
			state.statusText = "Checking agent profile…"
			return model, model.prepareFixCommand(state.target, &profile, state.generation)
		}
	case fixFieldModel:
		state.draft.Model = cycleAgentOption(state.draft.Probe.Capabilities.Models, state.draft.Model, direction)
	case fixFieldEffort:
		state.draft.Effort = cycleAgentOption(state.draft.Probe.Capabilities.Efforts, state.draft.Effort, direction)
	case fixFieldDelegation:
		state.draft.Delegation = cycleAgentOption(state.draft.Probe.Capabilities.Delegation, state.draft.Delegation, direction)
	case fixFieldScope:
		state.draft.ChangeScope = fixCycleString([]string{"targets-only", "targets-and-tests", "repository"}, state.draft.ChangeScope, direction)
		model.syncFixDraft()
	case fixFieldValidation:
		options := []string{""}
		for _, plan := range state.draft.Preferences.Validation {
			options = append(options, plan.ID)
		}
		state.draft.ValidationPlanID = fixCycleString(options, state.draft.ValidationPlanID, direction)
		model.syncFixDraft()
	case fixFieldDelivery:
		previous := state.draft.DeliveryMode
		state.draft.DeliveryMode = fix.DeliveryMode(fixCycleString([]string{"branch", "pull-request"}, string(state.draft.DeliveryMode), direction))
		model.syncFixDraft()
		if state.draft.DeliveryMode != previous {
			model.markFixDeliveryStale()
			model.ensureFixCursorVisible()
		}
	}
	return model, nil
}

func (model *Model) saveFixTargetPreference(preferences appconfig.Resolved) tea.Cmd {
	if model.configStore == nil {
		return nil
	}
	model.fixTargetSaving = true
	store, workspace := model.configStore, model.configWorkspace
	score, revision := model.fixTargetDesired, preferences.Revision
	defaults := cloneConfigFix(preferences.Fix)
	defaults.TargetScore = score
	return func() tea.Msg {
		saved, err := store.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Fix: &defaults}, revision)
		if errors.Is(err, appconfig.ErrRevisionConflict) {
			resolved, resolveErr := store.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
			if resolveErr != nil {
				err = resolveErr
			} else {
				defaults = cloneConfigFix(resolved.Fix)
				defaults.TargetScore = score
				saved, err = store.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Fix: &defaults}, resolved.Revision)
			}
		}
		return fixTargetPreferenceSavedMsg{score: score, saved: saved, err: err}
	}
}

func (model *Model) handleFixTargetPreferenceSaved(message fixTargetPreferenceSavedMsg) tea.Cmd {
	model.fixTargetSaving = false
	if message.err != nil {
		model.fixNotice = "Target score preference was not saved: " + cleanAgentText(message.err.Error())
	} else {
		model.fixDialog.draft.Preferences = message.saved.Resolved
	}
	if model.fixTargetDesired != message.score {
		preferences := model.fixDialog.draft.Preferences
		if message.err == nil {
			preferences = message.saved.Resolved
		}
		return model.saveFixTargetPreference(preferences)
	}
	return nil
}

func (model *Model) moveFixCursor(direction int) {
	fields := model.fixVisibleFields()
	if len(fields) == 0 {
		return
	}
	position := fixFieldPosition(fields, model.fixDialog.cursor)
	position = min(max(0, position+direction), len(fields)-1)
	model.fixDialog.cursor = fields[position]
}

func (model *Model) ensureFixCursorVisible() {
	fields := model.fixVisibleFields()
	if len(fields) == 0 {
		model.fixDialog.cursor = fixFieldTargetScore
		return
	}
	for _, field := range fields {
		if field == model.fixDialog.cursor {
			return
		}
	}
	model.fixDialog.cursor = fields[min(fixFieldPosition(fields, model.fixDialog.cursor), len(fields)-1)]
}

func cycleAgentOption[T ~string](options []agent.Option[T], current T, direction int) T {
	if len(options) == 0 {
		return current
	}
	index := 0
	for candidate := range options {
		if options[candidate].ID == current {
			index = candidate
		}
	}
	return options[fixCycleIndex(index, direction, len(options))].ID
}

func fixCycleString(options []string, current string, direction int) string {
	if len(options) == 0 {
		return current
	}
	index := 0
	for candidate := range options {
		if options[candidate] == current {
			index = candidate
		}
	}
	return options[fixCycleIndex(index, direction, len(options))]
}

func fixCycleIndex(current, direction, length int) int {
	if length <= 0 {
		return 0
	}
	return (current + direction%length + length) % length
}

func (model *Model) submitFix() (tea.Model, tea.Cmd) {
	if model.fixDialog.submitBlocked {
		model.fixDialog.errorText = "Submission readiness is stale · press R to recheck before retrying"
		return model, nil
	}
	if !model.syncFixDraft() {
		return model, nil
	}
	state := &model.fixDialog
	if !model.fixDialogRunnable() {
		state.errorText = "Run fix is unavailable: " + fixPreflightSummary(state.draft)
		return model, nil
	}
	state.submitting = true
	state.statusText = "Submitting fix…"
	service, generation, draft := model.fixService, state.generation, state.draft
	return model, func() tea.Msg {
		jobID, err := service.Submit(context.Background(), fixapp.SubmitRequest{Draft: draft})
		return fixSubmittedMsg{generation: generation, jobID: jobID, err: err}
	}
}

func (model *Model) handleFixSubmitted(message fixSubmittedMsg) {
	if message.generation != model.fixDialog.generation || !model.hasOverlay(OverlayFixForm) {
		return
	}
	model.fixDialog.submitting = false
	if message.err != nil {
		model.fixDialog.errorText = message.err.Error()
		model.fixDialog.statusText = "Submission blocked · R recheck readiness before retrying"
		model.fixDialog.submitBlocked = true
		return
	}
	model.overlays.Pop()
	model.status = ""
	model.switchMainView(MainViewAgents)
	model.agents.FindQuery = ""
	model.agents.Selected = AgentRowID{JobID: message.jobID}
}

func (model Model) fixDialogRunnable() bool {
	return model.fixDialog.hasDraft && !model.fixDialog.submitBlocked && !model.fixDialog.deliveryStale && fixDraftRunnable(model.fixDialog.draft)
}

func (model *Model) markFixDeliveryStale() {
	model.fixDialog.deliveryStale = true
	model.fixDialog.statusText = "Result changed · press R to recheck readiness"
}

func (model *Model) openFixRemediationSettings() tea.Cmd {
	kind, ok := model.fixRemediationSettingsKind()
	if !ok {
		model.fixDialog.statusText = "No settings remediation is available · resolve the diagnostic, then press R to recheck"
		return nil
	}
	diagnostic := model.fixDialog.errorText
	if diagnostic == "" && model.fixDialog.hasDraft {
		diagnostic = fixPreflightSummary(model.fixDialog.draft)
	}
	model.fixNotice = "Fix blocked: " + cleanAgentText(diagnostic)
	command := model.openConfigSettings(kind)
	model.configSettings.returnToFix = true
	model.overlays.Push(OverlayConfigSettings, OverlayCaller{
		MainView: MainViewFiles, Overlay: OverlayFixForm, Selected: model.fixDialog.target.String(),
	})
	return command
}

func (model Model) fixRemediationSettingsKind() (configSettingsKind, bool) {
	state := model.fixDialog
	if state.hasDraft {
		draft := state.draft
		if draft.Probe.State == agent.ProbeUnauthenticated || draft.Probe.State == agent.ProbeUnavailable || draft.Probe.State == agent.ProbeIncompatible {
			return configAgents, true
		}
		if draft.Probe.State == agent.ProbeDegraded || !draft.Probe.Capabilities.Isolation.EligibleForMutation() {
			return "", false
		}
		if !fixOptionContains(draft.Probe.Capabilities.Models, draft.Model) ||
			!fixOptionContains(draft.Probe.Capabilities.Efforts, draft.Effort) ||
			!fixOptionContains(draft.Probe.Capabilities.Delegation, draft.Delegation) {
			return configFix, true
		}
		if !fixValidationRunnable(draft) {
			return configValidation, true
		}
		if strings.TrimSpace(draft.BranchName) == "" {
			return configDelivery, true
		}
	}
	message := strings.ToLower(state.errorText)
	switch {
	case strings.Contains(message, "validation"):
		return configValidation, true
	case strings.Contains(message, "delivery"), strings.Contains(message, "branch"), strings.Contains(message, "remote"), strings.Contains(message, "pull request"):
		return configDelivery, true
	case strings.Contains(message, "agent"), strings.Contains(message, "profile"), strings.Contains(message, "runtime"), strings.Contains(message, "executable"),
		strings.Contains(message, "authentication"), strings.Contains(message, "codex"), strings.Contains(message, "claude"), strings.Contains(message, "grok"):
		return configAgents, true
	case strings.Contains(message, "config"), strings.Contains(message, "preference"):
		return configFix, true
	default:
		return "", false
	}
}

func (model Model) hasOverlay(kind OverlayKind) bool {
	for _, frame := range model.overlays.frames {
		if frame.Kind == kind {
			return true
		}
	}
	return false
}

func initialFixJobsCommand(service FixService) tea.Cmd {
	return func() tea.Msg {
		snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
		return fixJobsMsg{revision: snapshot.Revision, jobs: snapshot.Jobs}
	}
}

func waitFixJobsCommand(service FixService, subscription fixapp.Subscription, after fixapp.GlobalRevision) tea.Cmd {
	return func() tea.Msg {
		revision, err := subscription.Wait(context.Background(), after)
		if err != nil {
			return fixJobsMsg{revision: revision, err: err}
		}
		snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
		return fixJobsMsg{revision: snapshot.Revision, jobs: snapshot.Jobs}
	}
}

func (model *Model) handleFixJobs(message fixJobsMsg) tea.Cmd {
	if message.err != nil {
		model.fixNotice = "Fix updates unavailable: " + message.err.Error()
		model.fixUpdatesStale = true
		model.fixRetryGeneration++
		generation := model.fixRetryGeneration
		return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return fixRetrySubscriptionMsg{generation: generation} })
	}
	model.fixUpdatesStale = false
	if strings.HasPrefix(model.fixNotice, "Fix updates unavailable:") {
		model.fixNotice = ""
	}
	var monitorCommand tea.Cmd
	if message.revision >= model.fixRevision {
		model.fixRevision = message.revision
		model.setAgentPresentations(message.jobs)
		if model.hasOverlay(OverlayJobMonitor) {
			if job, ok := model.agentJobByID(model.jobMonitor.jobID); ok {
				model.jobMonitor.job = job
				monitorCommand = model.beginJobMonitorRefresh()
			}
		}
	}
	if model.fixService == nil || model.fixSubscription == nil {
		return monitorCommand
	}
	waitCommand := waitFixJobsCommand(model.fixService, model.fixSubscription, model.fixRevision)
	if monitorCommand != nil {
		return tea.Batch(waitCommand, monitorCommand)
	}
	return waitCommand
}

func (model *Model) retryFixSubscription(message fixRetrySubscriptionMsg) tea.Cmd {
	if message.generation != model.fixRetryGeneration || model.fixService == nil {
		return nil
	}
	if model.fixSubscription != nil {
		_ = model.fixSubscription.Close()
	}
	model.fixSubscription = model.fixService.Subscribe()
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeFinished: true})
	model.fixRevision = snapshot.Revision
	model.setAgentPresentations(snapshot.Jobs)
	model.fixUpdatesStale = false
	model.fixNotice = "Fix updates restored"
	return waitFixJobsCommand(model.fixService, model.fixSubscription, model.fixRevision)
}

func (model *Model) openJobMonitor(jobID fix.JobID, focus fix.RepoPath) tea.Cmd {
	if jobID == "" || model.fixService == nil {
		model.fixNotice = "Select a fix job to inspect"
		return nil
	}
	model.fixGeneration++
	model.jobMonitor = jobMonitorState{generation: model.fixGeneration, jobID: jobID, focusPath: focus, loading: true, refreshing: true}
	if !model.hasOverlay(OverlayJobMonitor) {
		model.overlays.Push(OverlayJobMonitor, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: jobID, Path: focus}.String()})
	}
	service, generation := model.fixService, model.fixGeneration
	return model.loadJobMonitorCommandWithService(service, jobID, generation)
}

func (model Model) loadJobMonitorCommand(jobID fix.JobID, generation uint64) tea.Cmd {
	return model.loadJobMonitorCommandWithService(model.fixService, jobID, generation)
}

// beginJobMonitorRefresh coalesces subscription bursts into at most one queued
// reload. Each actual reload receives a fresh generation so a late response can
// never replace a newer monitor snapshot.
func (model *Model) beginJobMonitorRefresh() tea.Cmd {
	if model.fixService == nil || model.jobMonitor.jobID == "" {
		return nil
	}
	if model.jobMonitor.refreshing {
		model.jobMonitor.pending = true
		return nil
	}
	model.fixGeneration++
	model.jobMonitor.generation = model.fixGeneration
	model.jobMonitor.refreshing = true
	return model.loadJobMonitorCommand(model.jobMonitor.jobID, model.jobMonitor.generation)
}

func (model Model) loadJobMonitorCommandWithService(service FixService, jobID fix.JobID, generation uint64) tea.Cmd {
	return func() tea.Msg {
		job, found := service.Job(jobID)
		if !found {
			return jobMonitorMsg{generation: generation, found: false}
		}
		return jobMonitorMsg{generation: generation, job: job, found: true}
	}
}

func (model *Model) handleJobMonitor(message jobMonitorMsg) tea.Cmd {
	if message.generation != model.jobMonitor.generation || !model.hasOverlay(OverlayJobMonitor) {
		return nil
	}
	model.jobMonitor.loading = false
	model.jobMonitor.refreshing = false
	if !message.found {
		model.jobMonitor.errorText = "Job is no longer available"
	} else if message.err != nil {
		model.jobMonitor.errorText = message.err.Error()
	} else {
		model.jobMonitor.errorText = ""
	}
	if message.found {
		model.jobMonitor.job = message.job
	}
	model.jobMonitor.offset = min(model.jobMonitor.offset, model.jobMonitorMaxOffset())
	if model.jobMonitor.pending {
		model.jobMonitor.pending = false
		return model.beginJobMonitorRefresh()
	}
	return nil
}

func (model *Model) handleJobMonitorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		model.overlays.Pop()
	case "up", "k":
		model.jobMonitor.offset = max(0, model.jobMonitor.offset-1)
	case "down", "j":
		model.jobMonitor.offset = min(model.jobMonitorMaxOffset(), model.jobMonitor.offset+1)
	case "pgup", "ctrl+b":
		model.jobMonitor.offset = max(0, model.jobMonitor.offset-max(1, model.height-6))
	case "pgdown", "ctrl+f":
		model.jobMonitor.offset = min(model.jobMonitorMaxOffset(), model.jobMonitor.offset+max(1, model.height-6))
	case "[", "]":
		jobs := model.visibleAgentJobs()
		if len(jobs) == 0 {
			return model, nil
		}
		index := 0
		for candidate := range jobs {
			if jobs[candidate].ID == model.jobMonitor.jobID {
				index = candidate
			}
		}
		delta := 1
		if key.String() == "[" {
			delta = -1
		}
		index = fixCycleIndex(index, delta, len(jobs))
		return model, model.openJobMonitor(jobs[index].ID, "")
	case "l":
		return model, model.openJobLog(model.jobMonitor.jobID)
	case "d":
		return model, model.openJobDiff(model.jobMonitor.jobID, model.jobMonitor.focusPath)
	case "C":
		return model.activateJobAction(model.jobMonitor.jobID, fix.ActionCancel)
	}
	return model, nil
}

func (model *Model) openJobLog(jobID fix.JobID) tea.Cmd {
	return model.openJobReader(OverlayJobLog, jobID, "")
}

func (model *Model) openJobDiff(jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	return model.openJobReader(OverlayJobDiff, jobID, path)
}

func (model *Model) openCandidateSource(jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	return model.openJobReader(OverlayCandidateSource, jobID, path)
}

func (model *Model) openJobReader(kind OverlayKind, jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	if jobID == "" || model.fixService == nil {
		model.fixNotice = "Job details are unavailable"
		return nil
	}
	model.fixGeneration++
	model.jobReader = jobReaderState{generation: model.fixGeneration, kind: kind, jobID: jobID, path: path, loading: true}
	model.overlays.Push(kind, OverlayCaller{MainView: MainViewAgents, Overlay: OverlayJobMonitor, Selected: AgentRowID{JobID: jobID, Path: path}.String()})
	service, generation := model.fixService, model.fixGeneration
	return func() tea.Msg {
		lines := []string{}
		truncated := false
		var err error
		switch kind {
		case OverlayJobLog:
			var page fixapp.LogPage
			page, err = loadRetainedTranscript(context.Background(), service, jobID)
			truncated = page.Truncated
			for _, entry := range page.Entries {
				at := entry.At.Format("15:04:05")
				lines = append(lines, fmt.Sprintf("%s  %-16s %s", at, entry.Kind, cleanAgentText(entry.Summary)))
			}
		case OverlayJobDiff:
			offset := 0
			fingerprint := ""
			for {
				var page fixapp.DiffPage
				page, err = service.Diff(context.Background(), jobID, fixapp.DiffRequest{Offset: offset, Limit: 200})
				if err != nil {
					break
				}
				if fingerprint == "" {
					fingerprint = page.Fingerprint
				} else if page.Fingerprint != fingerprint {
					err = errors.New("candidate diff changed while loading; press r to refresh")
					truncated = true
					break
				}
				for _, file := range page.Files {
					if path != "" && file.Path != path {
						continue
					}
					label := fmt.Sprintf("%-10s %s", cleanAgentText(file.Status), file.Path)
					if file.Additions > 0 || file.Deletions > 0 {
						label += fmt.Sprintf("  +%d -%d", file.Additions, file.Deletions)
					}
					if file.Previous != "" {
						label += "  from " + file.Previous.String()
					}
					if file.Binary {
						label += "  [binary]"
					}
					lines = append(lines, label)
				}
				if page.Complete {
					break
				}
				if page.NextOffset <= offset {
					err = errors.New("diff pagination did not advance")
					truncated = true
					break
				}
				offset = page.NextOffset
			}
		case OverlayCandidateSource:
			var file candidate.File
			file, err = service.CandidateFile(context.Background(), jobID, path)
			if err == nil {
				lines = strings.Split(strings.ReplaceAll(string(file.Contents), "\r\n", "\n"), "\n")
				truncated = file.Truncated
			}
		}
		return jobReaderMsg{generation: generation, kind: kind, lines: lines, truncated: truncated, err: err}
	}
}

func loadRetainedTranscript(ctx context.Context, service FixService, jobID fix.JobID) (fixapp.LogPage, error) {
	result := fixapp.LogPage{}
	cursor := fixapp.LogCursor(0)
	for {
		page, err := service.Transcript(ctx, jobID, cursor, 500)
		if err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, page.Entries...)
		result.Next = page.Next
		result.Truncated = result.Truncated || page.Truncated
		if page.Complete {
			result.Complete = true
			return result, nil
		}
		if page.Next <= cursor {
			return result, errors.New("activity pagination did not advance")
		}
		cursor = page.Next
	}
}

func (model *Model) handleJobReader(message jobReaderMsg) {
	if message.generation != model.jobReader.generation || message.kind != model.jobReader.kind || !model.hasOverlay(message.kind) {
		return
	}
	model.jobReader.loading = false
	model.jobReader.lines = append([]string(nil), message.lines...)
	model.jobReader.truncated = message.truncated
	if message.err != nil {
		model.jobReader.errorText = message.err.Error()
	}
}

func (model *Model) handleJobReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := max(1, model.height-5)
	maximum := max(0, len(model.jobReader.lines)-page)
	switch key.String() {
	case "esc", "q":
		model.overlays.Pop()
	case "up", "k":
		model.jobReader.offset = max(0, model.jobReader.offset-1)
	case "down", "j":
		model.jobReader.offset = min(maximum, model.jobReader.offset+1)
	case "pgup", "ctrl+b":
		model.jobReader.offset = max(0, model.jobReader.offset-page)
	case "pgdown", "ctrl+f":
		model.jobReader.offset = min(maximum, model.jobReader.offset+page)
	case "home", "g":
		model.jobReader.offset = 0
	case "end", "G":
		model.jobReader.offset = maximum
	case "r":
		model.overlays.Pop()
		return model, model.openJobReader(model.jobReader.kind, model.jobReader.jobID, model.jobReader.path)
	}
	return model, nil
}

func (model *Model) requestQuit() (tea.Model, tea.Cmd) {
	active := model.activeFixJobs()
	if len(active) == 0 || model.fixService == nil {
		return model, tea.Quit
	}
	model.shutdown = shutdownState{active: len(active)}
	model.overlays.Push(OverlayShutdown, OverlayCaller{MainView: model.mainView, Selected: model.mainSelection()})
	return model, nil
}

func (model Model) activeFixJobs() []fix.JobPresentation {
	result := make([]fix.JobPresentation, 0)
	for _, job := range model.agents.Jobs {
		switch job.Phase {
		case fix.PhaseAdmitted, fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing, fix.PhaseRunning,
			fix.PhaseWaitingVerifier, fix.PhaseVerifying, fix.PhasePublishing, fix.PhaseCanceling,
			fix.PhaseReconciling, fix.PhaseDiscarding:
			result = append(result, job)
		}
	}
	return result
}

func (model *Model) handleShutdownKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.shutdown.pending {
		return model, nil
	}
	switch key.String() {
	case "esc", "q":
		model.overlays.Pop()
		return model, nil
	case "enter", "y":
		model.shutdown.pending = true
		service := model.fixService
		return model, func() tea.Msg {
			return shutdownCompleteMsg{err: service.Shutdown(context.Background())}
		}
	}
	return model, nil
}

func (model *Model) handleShutdownComplete(message shutdownCompleteMsg) tea.Cmd {
	model.shutdown.pending = false
	if message.err != nil {
		model.shutdown.errorText = message.err.Error()
		return nil
	}
	return tea.Quit
}

func (model *Model) openCancelConfirmation() {
	jobID := model.agents.Selected.JobID
	if jobID == "" {
		model.fixNotice = "Select a job row to cancel it"
		return
	}
	_, _ = model.activateJobAction(jobID, fix.ActionCancel)
}

func (model *Model) activateJobAction(jobID fix.JobID, choices ...fix.JobAction) (tea.Model, tea.Cmd) {
	job, ok := model.agentJobByID(jobID)
	if !ok {
		model.fixNotice = "Selected job is no longer available"
		return model, nil
	}
	action := fix.JobAction("")
	for _, choice := range choices {
		if containsFixAction(job.AllowedActions, choice) {
			action = choice
			break
		}
	}
	if action == "" {
		model.fixNotice = jobActionLabel(choices[0]) + " is unavailable for this job"
		return model, nil
	}
	if jobActionRequiresConfirmation(action) {
		model.cancelConfirmation = cancelConfirmation{
			jobID: job.ID, revision: job.Revision, action: action,
			deliveryMode: job.DeliveryMode, branchName: job.BranchName,
			diffHash: job.DiffFingerprint, allowed: true,
		}
		model.overlays.Push(OverlayConfirmation, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: job.ID}.String()})
		return model, nil
	}
	model.jobCommand = jobCommandState{jobID: job.ID, revision: job.Revision, action: action, pending: true}
	return model.executeSelectedJobAction(job.ID, job.Revision, action, false)
}

func (model Model) agentJobByID(id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range model.agents.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func jobActionRequiresConfirmation(action fix.JobAction) bool {
	return action == fix.ActionCancel
}

func (model Model) selectedAgentJob() (fix.JobPresentation, bool) {
	for _, job := range model.agents.Jobs {
		if job.ID == model.agents.Selected.JobID {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func containsFixAction(actions []fix.JobAction, wanted fix.JobAction) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func (model *Model) handleCancelConfirmationKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (key.String() == "esc" || key.String() == "q") && !model.cancelConfirmation.pending {
		model.overlays.Pop()
		return model, nil
	}
	if key.String() != "enter" || model.cancelConfirmation.pending || !model.cancelConfirmation.allowed {
		return model, nil
	}
	return model.executeSelectedJobAction(model.cancelConfirmation.jobID, model.cancelConfirmation.revision, model.cancelConfirmation.action, true)
}

func (model *Model) executeSelectedJobAction(jobID fix.JobID, revision uint64, action fix.JobAction, confirmation bool) (tea.Model, tea.Cmd) {
	requestID, err := fix.NewCommandID()
	if err != nil {
		if confirmation {
			model.cancelConfirmation.errorText = err.Error()
		} else {
			model.fixNotice = "Could not create job command: " + err.Error()
		}
		return model, nil
	}
	if confirmation {
		model.cancelConfirmation.pending = true
	} else {
		model.jobCommand.pending = true
	}
	service := model.fixService
	diffHash := ""
	if confirmation {
		diffHash = model.cancelConfirmation.diffHash
	} else if job, ok := model.agentJobByID(jobID); ok {
		diffHash = job.DiffFingerprint
	}
	command := fix.JobCommand{RequestID: requestID, JobID: jobID, ExpectedRevision: revision, Action: action, DiffHash: diffHash}
	return model, func() tea.Msg {
		receipt, executeErr := service.Execute(context.Background(), command)
		return fixCommandMsg{jobID: command.JobID, action: action, receipt: receipt, err: executeErr}
	}
}

func (model *Model) handleFixCommand(message fixCommandMsg) {
	confirmation := model.hasOverlay(OverlayConfirmation) && message.jobID == model.cancelConfirmation.jobID && message.action == model.cancelConfirmation.action
	direct := !confirmation && model.jobCommand.pending && message.jobID == model.jobCommand.jobID && message.action == model.jobCommand.action
	if !confirmation && !direct {
		return
	}
	if confirmation {
		model.cancelConfirmation.pending = false
	} else {
		model.jobCommand.pending = false
	}
	if message.err != nil {
		model.refreshActionAfterError(message, confirmation)
		return
	}
	if !message.receipt.Accepted {
		if confirmation {
			model.cancelConfirmation.errorText = message.receipt.Message
		} else {
			model.fixNotice = message.receipt.Message
		}
		return
	}
	if confirmation {
		model.overlays.Pop()
	}
	if message.action == fix.ActionCancel {
		for index := range model.agents.Jobs {
			if model.agents.Jobs[index].ID != message.jobID {
				continue
			}
			model.agents.Jobs[index].Phase = fix.PhaseCanceling
			model.agents.Jobs[index].Attention = fix.AttentionNone
			model.agents.Jobs[index].CurrentAction = "Canceling"
			model.agents.Jobs[index].AllowedActions = nil
			model.agents.Jobs[index].Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
			if message.receipt.Revision > 0 {
				model.agents.Jobs[index].Revision = message.receipt.Revision
			}
			break
		}
	}
	model.fixNotice = jobActionPastTense(message.action) + " requested for " + string(message.jobID)
}

func (model *Model) refreshActionAfterError(message fixCommandMsg, confirmation bool) {
	errorText := message.err.Error()
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeFinished: true})
	model.setAgentPresentations(snapshot.Jobs)
	for _, job := range snapshot.Jobs {
		if job.ID != message.jobID {
			continue
		}
		if confirmation {
			model.cancelConfirmation.revision = job.Revision
			model.cancelConfirmation.deliveryMode = job.DeliveryMode
			model.cancelConfirmation.branchName = job.BranchName
			model.cancelConfirmation.diffHash = job.DiffFingerprint
			model.cancelConfirmation.allowed = containsFixAction(job.AllowedActions, message.action)
			if model.cancelConfirmation.allowed {
				model.cancelConfirmation.errorText = errorText + "; job changed, review and confirm again"
			} else {
				model.cancelConfirmation.errorText = errorText + "; this job can no longer be canceled"
			}
		} else {
			model.jobCommand.revision = job.Revision
			if containsFixAction(job.AllowedActions, message.action) {
				model.fixNotice = errorText + "; job changed, press the hotkey again"
			} else {
				model.fixNotice = errorText + "; " + strings.ToLower(jobActionLabel(message.action)) + " is no longer available"
			}
		}
		return
	}
	if confirmation {
		model.cancelConfirmation.errorText = errorText + "; job is no longer available"
	} else {
		model.fixNotice = errorText + "; job is no longer available"
	}
}

func jobActionPastTense(action fix.JobAction) string {
	if action == fix.ActionCancel {
		return "Cancel"
	}
	return "Action"
}
