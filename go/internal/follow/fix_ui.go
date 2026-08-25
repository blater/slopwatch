package follow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
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
	fixFieldAdvanced
	fixFieldRun
	fixFieldCount
)

type fixDialogState struct {
	generation         uint64
	target             fix.RepoPath
	draft              fixapp.FixDraft
	hasDraft           bool
	loading            bool
	submitting         bool
	cursor             int
	metricCursor       int
	focus              map[fix.MetricID]bool
	metrics            []fix.MetricID
	branch             textinput.Model
	prompt             textarea.Model
	branchOriginal     string
	promptOriginal     string
	detached           bool
	promptPreview      bool
	promptResetPending bool
	promptDirtyCursor  int
	branchEditing      bool
	deliveryStale      bool
	submitBlocked      bool
	errorText          string
	statusText         string
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

type jobActionsState struct {
	jobID     fix.JobID
	revision  uint64
	actions   []fix.JobAction
	cursor    int
	pending   bool
	errorText string
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
	prompt := textarea.New()
	prompt.Placeholder = "Additional guidance for the agent"
	model.fixDialog = fixDialogState{
		generation: model.fixGeneration, target: path, loading: true, focus: map[fix.MetricID]bool{}, branch: branch, prompt: prompt,
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
		if job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseArchived || job.Phase == fix.PhaseDiscarded {
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

func (model Model) fixCodeForPath(path string) string {
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
	if selected.Phase == fix.PhaseAwaitingAction {
		if selected.Issue != nil && strings.EqualFold(selected.Issue.Code, "canceled") {
			return "CAN"
		}
		if selected.Scope == fix.ScopeConflicted || selected.ConflictCount > 0 {
			return "CNF"
		}
		return "ERR"
	}
	switch selected.Phase {
	case fix.PhaseAdmitted, fix.PhaseQueued:
		return "QUE"
	case fix.PhasePreflight:
		return "PFL"
	case fix.PhasePreparing:
		return "PRE"
	case fix.PhaseRunning:
		return "RUN"
	case fix.PhaseWaitingVerifier:
		return "WTG"
	case fix.PhaseVerifying:
		return "VER"
	case fix.PhaseAwaitingReview:
		return "REV"
	case fix.PhasePublishing:
		return "PUB"
	case fix.PhaseCanceling:
		return "CNL"
	case fix.PhaseReconciling:
		return "REC"
	case fix.PhaseCompleted:
		return "DON"
	case fix.PhaseArchived:
		return "ARC"
	case fix.PhaseDiscarding:
		return "DSC"
	case fix.PhaseDiscarded:
		return "DIS"
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
	model.fixDialog.focus = map[fix.MetricID]bool{}
	for _, goal := range message.draft.Focus {
		model.fixDialog.focus[goal.Metric] = true
	}
	if old.hasDraft {
		edits := old.fixDraftEdits()
		if revised, err := fixapp.ReviseDraft(message.draft, edits); err == nil {
			model.fixDialog.draft = revised
		}
		model.fixDialog.focus = old.focus
		model.fixDialog.prompt.SetValue(old.prompt.Value())
		model.fixDialog.branch.SetValue(old.branch.Value())
		model.fixDialog.promptOriginal = old.promptOriginal
		model.fixDialog.branchOriginal = old.branchOriginal
		model.fixDialog.detached = old.detached
	} else {
		body := generatedFixBody(message.draft)
		if message.draft.Instructions.DetachedBody != "" {
			body = message.draft.Instructions.DetachedBody
			model.fixDialog.detached = true
		}
		model.fixDialog.prompt.SetValue(body)
		model.fixDialog.branch.SetValue(message.draft.BranchName)
		model.fixDialog.promptOriginal = body
		model.fixDialog.branchOriginal = message.draft.BranchName
	}
	if fixValidationNeedsRepair(model.fixDialog.draft) {
		model.fixDialog.cursor = fixFieldValidation
	}
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
	result := make([]fix.MetricID, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func (state fixDialogState) fixDraftEdits() fixapp.DraftEdits {
	goals := make([]fix.MetricGoal, 0, len(state.focus))
	for _, id := range state.metrics {
		if !state.focus[id] {
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
		BranchName: state.branch.Value(), Guidance: state.draft.Instructions.UserGuidance,
		DetachedBody: func() string {
			if state.detached {
				return state.prompt.Value()
			}
			return ""
		}(),
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
		if name == "r" && !state.loading && !state.submitting {
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
	case "P":
		state.promptPreview = true
		state.prompt.Blur()
		model.overlays.Push(OverlayPromptEditor, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayFixForm, Selected: state.target.String()})
	case "up", "k":
		state.cursor = max(0, state.cursor-1)
	case "down", "j":
		state.cursor = min(fixFieldCount-1, state.cursor+1)
	case "left":
		return model.adjustFixField(-1)
	case "right":
		return model.adjustFixField(1)
	case " ":
		if state.cursor == fixFieldFocus && len(state.metrics) > 0 {
			id := state.metrics[state.metricCursor]
			state.focus[id] = !state.focus[id]
			model.syncFixDraft()
		}
	case "enter":
		switch state.cursor {
		case fixFieldBranch:
			state.branchOriginal = state.branch.Value()
			state.branchEditing = true
			state.branch.Focus()
		case fixFieldAdvanced:
			state.promptPreview = false
			state.prompt.Focus()
			state.prompt.SetWidth(max(20, model.width-4))
			state.prompt.SetHeight(max(3, model.height-6))
			model.overlays.Push(OverlayPromptEditor, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayFixForm, Selected: state.target.String()})
		case fixFieldRun:
			return model.submitFix()
		}
	}
	return model, nil
}

func (model *Model) adjustFixField(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch state.cursor {
	case fixFieldTargetScore:
		state.draft.TargetScore = max(0, state.draft.TargetScore+float64(direction*10))
		model.syncFixDraft()
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
		state.draft.ChangeScope = fixCycleString([]string{"targets-only", "targets-and-tests"}, state.draft.ChangeScope, direction)
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
		state.draft.DeliveryMode = fix.DeliveryMode(fixCycleString(fixUniqueStrings(string(state.draft.DeliveryMode), "candidate", "branch", "pull-request"), string(state.draft.DeliveryMode), direction))
		model.syncFixDraft()
		if state.draft.DeliveryMode != previous {
			model.markFixDeliveryStale()
		}
	}
	return model, nil
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

func fixUniqueStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (model *Model) handlePromptEditorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.fixDialog.promptPreview {
		switch key.String() {
		case "esc", "q":
			model.overlays.Pop()
		case "e", "enter":
			model.fixDialog.promptPreview = false
			model.fixDialog.prompt.Focus()
		}
		return model, nil
	}
	if key.String() != "R" {
		model.fixDialog.promptResetPending = false
	}
	switch key.String() {
	case "R":
		if !model.fixDialog.detached {
			return model, nil
		}
		if !model.fixDialog.promptResetPending {
			model.fixDialog.promptResetPending = true
			return model, nil
		}
		model.resetPromptFromControls()
		return model, nil
	case "ctrl+s":
		if !model.fixDialog.detached && model.fixDialog.prompt.Value() != model.fixDialog.promptOriginal {
			model.overlays.Push(OverlayPromptDetach, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayPromptEditor, Selected: model.fixDialog.target.String()})
			return model, nil
		}
		model.applyPromptEdits()
		return model, nil
	case "esc":
		if model.fixDialog.prompt.Value() != model.fixDialog.promptOriginal {
			model.fixDialog.promptDirtyCursor = 0
			model.overlays.Push(OverlayPromptDirty, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayPromptEditor, Selected: model.fixDialog.target.String()})
		} else {
			model.fixDialog.prompt.Blur()
			model.overlays.Pop()
		}
		return model, nil
	default:
		updated, command := model.fixDialog.prompt.Update(key)
		model.fixDialog.prompt = updated
		return model, command
	}
}

func (model *Model) applyPromptEdits() {
	model.fixDialog.prompt.Blur()
	model.fixDialog.detached = true
	model.syncFixDraft()
	model.fixDialog.promptOriginal = model.fixDialog.prompt.Value()
	model.fixDialog.promptResetPending = false
	model.overlays.Pop()
}

func (model *Model) resetPromptFromControls() {
	body := generatedFixBody(model.fixDialog.draft)
	model.fixDialog.prompt.SetValue(body)
	model.fixDialog.detached = false
	model.fixDialog.promptResetPending = false
	model.syncFixDraft()
	model.fixDialog.promptOriginal = body
}

func (model *Model) handlePromptDetachKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		model.overlays.Pop()
	case "enter":
		model.overlays.Pop()
		model.applyPromptEdits()
	}
	return model, nil
}

func (model *Model) handlePromptDirtyKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		model.fixDialog.promptDirtyCursor = max(0, model.fixDialog.promptDirtyCursor-1)
	case "down", "j":
		model.fixDialog.promptDirtyCursor = min(2, model.fixDialog.promptDirtyCursor+1)
	case "esc", "q":
		model.overlays.Pop()
	case "enter":
		switch model.fixDialog.promptDirtyCursor {
		case 0:
			model.overlays.Pop()
			if !model.fixDialog.detached {
				model.overlays.Push(OverlayPromptDetach, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayPromptEditor, Selected: model.fixDialog.target.String()})
			} else {
				model.applyPromptEdits()
			}
		case 1:
			model.fixDialog.prompt.SetValue(model.fixDialog.promptOriginal)
			model.fixDialog.prompt.Blur()
			model.overlays.Pop()
			model.overlays.Pop()
		case 2:
			model.overlays.Pop()
			model.fixDialog.prompt.Focus()
		}
	}
	return model, nil
}

func generatedFixBody(draft fixapp.FixDraft) string {
	body := draft.Instructions.Objective
	if draft.Instructions.Evidence != "" {
		body += "\n\n" + draft.Instructions.Evidence
	}
	if draft.Instructions.UserGuidance != "" {
		body += "\n\nAdditional guidance:\n" + draft.Instructions.UserGuidance
	}
	return strings.TrimSpace(body)
}

func (model *Model) submitFix() (tea.Model, tea.Cmd) {
	if model.fixDialog.submitBlocked {
		model.fixDialog.errorText = "Submission readiness is stale · press r to recheck before retrying"
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
		model.fixDialog.statusText = "Submission blocked · r recheck readiness before retrying"
		model.fixDialog.submitBlocked = true
		return
	}
	model.overlays.Pop()
	model.status = fmt.Sprintf("Fix %s admitted", message.jobID)
	model.switchMainView(MainViewAgents)
	model.agents.FindQuery = ""
	model.agents.Selected = AgentRowID{JobID: message.jobID}
}

func (model Model) fixDialogRunnable() bool {
	return model.fixDialog.hasDraft && !model.fixDialog.submitBlocked && !model.fixDialog.deliveryStale && fixDraftRunnable(model.fixDialog.draft)
}

func (model *Model) markFixDeliveryStale() {
	model.fixDialog.deliveryStale = true
	model.fixDialog.statusText = "Delivery changed · press r to recheck readiness"
}

func (model *Model) openFixRemediationSettings() tea.Cmd {
	kind, ok := model.fixRemediationSettingsKind()
	if !ok {
		model.fixDialog.statusText = "No settings remediation is available · resolve the diagnostic, then press r to recheck"
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
		if draft.DeliveryMode != fix.DeliveryModeCandidate && strings.TrimSpace(draft.BranchName) == "" {
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
		snapshot := service.Jobs(fixapp.JobFilter{IncludeArchived: true})
		return fixJobsMsg{revision: snapshot.Revision, jobs: snapshot.Jobs}
	}
}

func waitFixJobsCommand(service FixService, subscription fixapp.Subscription, after fixapp.GlobalRevision) tea.Cmd {
	return func() tea.Msg {
		revision, err := subscription.Wait(context.Background(), after)
		if err != nil {
			return fixJobsMsg{revision: revision, err: err}
		}
		snapshot := service.Jobs(fixapp.JobFilter{IncludeArchived: true})
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
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeArchived: true})
	model.fixRevision = snapshot.Revision
	model.setAgentPresentations(snapshot.Jobs)
	model.fixUpdatesStale = false
	model.fixNotice = "Fix updates restored"
	return waitFixJobsCommand(model.fixService, model.fixSubscription, model.fixRevision)
}

func (model *Model) openJobMonitor(jobID fix.JobID, focus fix.RepoPath) tea.Cmd {
	if jobID == "" || model.fixService == nil {
		model.fixNotice = "Select a fix job to monitor"
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
		activity, err := loadRetainedTranscript(context.Background(), service, jobID)
		if errors.Is(err, fixapp.ErrJobNotFound) {
			err = nil
		}
		return jobMonitorMsg{generation: generation, job: job, activity: activity, found: true, err: err}
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
		model.jobMonitor.activity = append([]fixapp.LogEntry(nil), message.activity.Entries...)
		model.jobMonitor.activityTruncated = message.activity.Truncated
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
	case "enter", " ":
		model.agents.Selected = AgentRowID{JobID: model.jobMonitor.jobID}
		model.openJobActions()
	case "l":
		return model, model.openJobLog(model.jobMonitor.jobID)
	case "d":
		return model, model.openJobDiff(model.jobMonitor.jobID, model.jobMonitor.focusPath)
	case "C":
		model.agents.Selected = AgentRowID{JobID: model.jobMonitor.jobID}
		model.openCancelConfirmation()
	case "r":
		return model, model.openJobMonitor(model.jobMonitor.jobID, model.jobMonitor.focusPath)
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
	if !model.agents.Selected.IsJob() {
		model.fixNotice = "Select a job row to cancel it"
		return
	}
	job, ok := model.selectedAgentJob()
	if !ok || !containsFixAction(job.AllowedActions, fix.ActionCancel) {
		model.fixNotice = "Cancel is unavailable for the selected job"
		return
	}
	model.cancelConfirmation = cancelConfirmation{jobID: job.ID, revision: job.Revision, action: fix.ActionCancel, deliveryMode: job.DeliveryMode, branchName: job.BranchName, diffHash: job.DiffFingerprint, allowed: true}
	model.overlays.Push(OverlayConfirmation, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: job.ID}.String()})
}

func (model *Model) openJobActions() {
	if !model.agents.Selected.IsJob() {
		model.fixNotice = "Select a job row to open Actions"
		return
	}
	job, ok := model.selectedAgentJob()
	if !ok {
		model.fixNotice = "Selected job is no longer available"
		return
	}
	model.jobActions = jobActionsState{
		jobID: job.ID, revision: job.Revision, actions: supportedJobActions(job.AllowedActions),
	}
	model.overlays.Push(OverlayJobActions, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: job.ID}.String()})
}

func supportedJobActions(values []fix.JobAction) []fix.JobAction {
	supported := map[fix.JobAction]bool{
		fix.ActionCancel: true, fix.ActionRetry: true, fix.ActionResume: true, fix.ActionPublish: true,
		fix.ActionKeep: true, fix.ActionArchive: true, fix.ActionDiscard: true, fix.ActionCleanup: true,
		fix.ActionAcknowledgeConflict: true,
	}
	result := make([]fix.JobAction, 0, len(values))
	for _, value := range values {
		if supported[value] {
			result = append(result, value)
		}
	}
	return result
}

func (model *Model) handleJobActionsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.jobActions
	switch key.String() {
	case "esc", "q":
		if !state.pending {
			model.overlays.Pop()
		}
		return model, nil
	case "up", "k":
		if !state.pending && len(state.actions) > 0 {
			state.cursor = max(0, state.cursor-1)
		}
	case "down", "j":
		if !state.pending && len(state.actions) > 0 {
			state.cursor = min(len(state.actions)-1, state.cursor+1)
		}
	case "home", "g":
		state.cursor = 0
	case "end", "G":
		state.cursor = max(0, len(state.actions)-1)
	case "enter":
		if state.pending || len(state.actions) == 0 {
			return model, nil
		}
		action := state.actions[state.cursor]
		if jobActionRequiresConfirmation(action) {
			job, _ := model.agentJobByID(state.jobID)
			model.cancelConfirmation = cancelConfirmation{
				jobID: state.jobID, revision: state.revision, action: action,
				deliveryMode: job.DeliveryMode, branchName: job.BranchName, allowed: true,
				diffHash: job.DiffFingerprint,
			}
			model.overlays.Push(OverlayConfirmation, OverlayCaller{MainView: MainViewAgents, Overlay: OverlayJobActions, Selected: AgentRowID{JobID: state.jobID}.String()})
			return model, nil
		}
		return model.executeSelectedJobAction(state.jobID, state.revision, action, false)
	}
	return model, nil
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
	switch action {
	case fix.ActionPublish, fix.ActionDiscard, fix.ActionCleanup, fix.ActionCancel:
		return true
	default:
		return false
	}
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
			model.jobActions.errorText = err.Error()
		}
		return model, nil
	}
	if confirmation {
		model.cancelConfirmation.pending = true
	} else {
		model.jobActions.pending = true
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
	actionSheet := model.hasOverlay(OverlayJobActions) && message.jobID == model.jobActions.jobID
	if !confirmation && !actionSheet {
		return
	}
	if confirmation {
		model.cancelConfirmation.pending = false
	} else {
		model.jobActions.pending = false
	}
	if message.err != nil {
		model.refreshActionAfterError(message, confirmation)
		return
	}
	if !message.receipt.Accepted {
		if confirmation {
			model.cancelConfirmation.errorText = message.receipt.Message
		} else {
			model.jobActions.errorText = message.receipt.Message
		}
		return
	}
	model.overlays.Pop()
	if confirmation {
		if caller, ok := model.overlays.Top(); ok && caller.Kind == OverlayJobActions {
			model.overlays.Pop()
		}
	}
	model.fixNotice = jobActionPastTense(message.action) + " requested for " + string(message.jobID)
}

func (model *Model) refreshActionAfterError(message fixCommandMsg, confirmation bool) {
	errorText := message.err.Error()
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeArchived: true})
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
			if model.hasOverlay(OverlayJobActions) && model.jobActions.jobID == job.ID {
				model.jobActions.revision = job.Revision
				model.jobActions.actions = supportedJobActions(job.AllowedActions)
				model.jobActions.cursor = min(model.jobActions.cursor, max(0, len(model.jobActions.actions)-1))
			}
			if model.cancelConfirmation.allowed {
				model.cancelConfirmation.errorText = errorText + "; job changed, review and confirm again"
			} else {
				model.cancelConfirmation.errorText = errorText + "; this action is no longer allowed"
			}
		} else {
			model.jobActions.revision = job.Revision
			model.jobActions.actions = supportedJobActions(job.AllowedActions)
			model.jobActions.cursor = min(model.jobActions.cursor, max(0, len(model.jobActions.actions)-1))
			model.jobActions.errorText = errorText + "; actions refreshed, choose again"
		}
		return
	}
	if confirmation {
		model.cancelConfirmation.errorText = errorText + "; job is no longer available"
	} else {
		model.jobActions.errorText = errorText + "; job is no longer available"
	}
}

func jobActionPastTense(action fix.JobAction) string {
	switch action {
	case fix.ActionPublish:
		return "Publish"
	case fix.ActionDiscard, fix.ActionCleanup:
		return "Cleanup"
	case fix.ActionCancel:
		return "Cancel"
	case fix.ActionRetry:
		return "Retry"
	case fix.ActionResume:
		return "Resume"
	case fix.ActionKeep:
		return "Keep"
	case fix.ActionArchive:
		return "Archive"
	default:
		return "Action"
	}
}
