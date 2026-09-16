package follow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/style"
)

// FixService is the narrow application boundary consumed by follow mode. It
// deliberately exposes projections and commands, never cache, provider, Git,
// preference-file, or authorization internals.
type FixService interface {
	LoadFix(context.Context, fixapp.LoadRequest) (fixapp.FixInput, error)
	Run(context.Context, fixapp.FixInput) (fix.JobID, error)
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
	fixFieldScope
	fixFieldWorkspace
	fixFieldGit
	fixFieldPublish
	fixFieldBranch
	fixFieldCount
)

type fixDialogState struct {
	generation     uint64
	target         fix.RepoPath
	targets        []fix.RepoPath
	input          fixapp.FixInput
	hasInput       bool
	loading        bool
	starting       bool
	cursor         int
	focus          map[fix.MetricID]bool
	metrics        []fix.MetricID
	choiceOpen     bool
	choiceField    int
	choiceCursor   int
	score          textinput.Model
	scoreOriginal  float64
	scoreEditing   bool
	scoreError     string
	branch         textinput.Model
	branchOriginal string
	branchEditing  bool
	errorText      string
	statusText     string
}

type targetScoreEditResult struct {
	accepted bool
	canceled bool
	value    float64
}

type cancelConfirmation struct {
	jobID     fix.JobID
	action    fix.JobAction
	allowed   bool
	pending   bool
	errorText string
}

type jobCommandState struct {
	jobID   fix.JobID
	action  fix.JobAction
	pending bool
}

type jobMonitorState struct {
	generation uint64
	jobID      fix.JobID
	focusPath  fix.RepoPath
	job        fix.JobPresentation
	activity   []fixapp.LogEntry
	loading    bool
	refreshing bool
	pending    bool
	errorText  string
	offset     int
}

type jobReaderState struct {
	generation uint64
	kind       OverlayKind
	jobID      fix.JobID
	path       fix.RepoPath
	lines      []string
	loading    bool
	refreshing bool
	pending    bool
	follow     bool
	truncated  bool
	errorText  string
	offset     int
	horizontal int
	logCursor  fixapp.LogCursor
}

type shutdownState struct {
	active    int
	pending   bool
	errorText string
}

type fixLoadedMsg struct {
	generation uint64
	input      fixapp.FixInput
	err        error
}

type fixStartedMsg struct {
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
	jobs []fix.JobPresentation
	err  error
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
	logCursor  fixapp.LogCursor
	increment  bool
	truncated  bool
	err        error
}

type shutdownCompleteMsg struct{ err error }

func (model *Model) openFixForSelected() tea.Cmd {
	targets, err := selectedFixTargets(fixTargetSelection{
		Marked: model.files.markedPaths(model.options.Limit), Selected: model.files.Selected,
		Workspace: model.options.Workspace, RepositoryRoot: model.fixWorkspace.RepositoryRoot,
	})
	if err != nil {
		model.status = "Fix unavailable for this row: " + err.Error()
		return nil
	}
	path := targets[0]
	if len(targets) == 1 {
		if job, ok := existingFixForTarget(model.agents.Jobs, path); ok {
			model.switchMainView(MainViewAgents)
			model.agents.FindQuery = ""
			model.agents.Selected = AgentRowID{JobID: job.ID}
			model.agents.ensureVisible(makeAgentLayout(model.width, model.height, model.bodyHeight()))
			model.fixNotice = "Opened existing fix for " + path.String()
			return nil
		}
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
	style.ApplyTextInputStyle(&branch, false)
	score := textinput.New()
	score.Prompt = ""
	style.ApplyTextInputStyle(&score, false)
	model.fixDialog = fixDialogState{
		generation: model.fixGeneration, target: path, targets: append([]fix.RepoPath(nil), targets...), loading: true, focus: map[fix.MetricID]bool{}, score: score, branch: branch,
		statusText: "Preparing analysis of " + markedFilesLabel(len(targets)) + "…",
	}
	model.overlays.Push(OverlayFixForm, OverlayCaller{MainView: MainViewFiles, Selected: model.files.Selected})
	return model.loadFixCommand(targets, nil, model.fixGeneration)
}

func (model Model) loadFixCommand(paths []fix.RepoPath, profile *agent.ProfileID, generation uint64) tea.Cmd {
	service := model.fixService
	workspace := model.fixWorkspace
	var selectedDelivery *fixapp.LoadDelivery
	if model.fixDialog.hasInput {
		selectedDelivery = &fixapp.LoadDelivery{Plan: model.fixDialog.input.DeliveryPlan, Branch: model.fixDialog.input.BranchName}
	}
	if workspace.RepositoryRoot == "" {
		workspace.RepositoryRoot = model.options.Workspace
		workspace.AnalysisRoot = model.options.Workspace
	}
	return func() tea.Msg {
		overrides := appconfig.SessionOverrides{Profile: profile}
		input, err := service.LoadFix(context.Background(), fixapp.LoadRequest{
			Workspace: workspace, Targets: append([]fix.RepoPath(nil), paths...), Overrides: overrides, Delivery: selectedDelivery,
		})
		return fixLoadedMsg{generation: generation, input: input, err: err}
	}
}

func (model *Model) handleFixLoaded(message fixLoadedMsg) {
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
	model.fixDialog.input = message.input
	model.fixDialog.hasInput = true
	model.fixDialog.errorText = ""
	model.fixDialog.statusText = "Ready"
	model.fixDialog.metrics = availableFixMetrics(message.input)
	model.fixDialog.focus = map[fix.MetricID]bool{}
	for _, goal := range message.input.Focus {
		model.fixDialog.focus[goal.Metric] = true
	}
	if old.hasInput {
		if old.input.Model != message.input.Model || old.input.Effort != message.input.Effort {
			model.fixDialog.statusText = fmt.Sprintf("Profile changed · using %s / %s", message.input.Model, message.input.Effort)
		}
		edits := old.fixFormValues()
		if revised, err := fixapp.ApplyFormValues(message.input, edits); err == nil {
			model.fixDialog.input = revised
		}
		model.fixDialog.focus = old.focus
		model.fixDialog.branch.SetValue(old.branch.Value())
		model.fixDialog.branchOriginal = old.branchOriginal
	} else {
		model.fixDialog.branch.SetValue(message.input.BranchName)
		model.fixDialog.branchOriginal = message.input.BranchName
	}
	model.fixDialog.ensureCursorVisible()
}

func (model *Model) openFixTargetScoreEditor() tea.Cmd {
	state := &model.fixDialog
	state.scoreOriginal = state.input.TargetScore
	state.score = textinput.New()
	state.score.Prompt = ""
	state.score.Width = 12
	state.score.CharLimit = 32
	style.ApplyTextInputStyle(&state.score, true)
	state.score.SetValue(formatTargetScore(state.input.TargetScore))
	state.score.CursorEnd()
	state.scoreEditing = true
	state.scoreError = ""
	model.overlays.Push(OverlayTargetScoreEditor, OverlayCaller{MainView: model.mainView, Overlay: OverlayFixForm, Selected: model.mainSelection()})
	return state.score.Focus()
}

func (model *Model) handleFixTargetScoreKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	result, command := model.fixDialog.handleTargetScoreKey(key)
	if result.canceled {
		model.overlays.Pop()
		return model, command
	}
	if !result.accepted {
		return model, command
	}
	model.overlays.Pop()
	model.fixTargetDesired = result.value
	if model.fixTargetSaving {
		return model, command
	}
	return model, model.saveFixTargetPreference(model.fixDialog.input.Preferences)
}

type fixDialogChoice struct {
	value    string
	label    string
	selected bool
	disabled bool
}

func (model *Model) handleFixChoiceKey(name string) (tea.Model, tea.Cmd) {
	choice, selected := model.fixDialog.handleChoiceKey(name)
	if !selected {
		return model, nil
	}
	return model.applyFixChoice(choice)
}

func (model *Model) applyFixChoice(choice fixDialogChoice) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch state.choiceField {
	case fixFieldFocus:
		id := fix.MetricID(choice.value)
		state.focus[id] = !state.focus[id]
		model.fixDialog.syncInput()
	case fixFieldProfile:
		profile := agent.ProfileID(choice.value)
		state.choiceOpen = false
		if profile == state.input.Profile.ID {
			return model, nil
		}
		model.fixGeneration++
		state.generation = model.fixGeneration
		state.loading = true
		state.statusText = "Checking agent profile…"
		return model, model.loadFixCommand(state.targetPaths(), &profile, state.generation)
	case fixFieldModel:
		state.input.Model = agent.ModelID(choice.value)
		state.choiceOpen = false
	case fixFieldEffort:
		state.input.Effort = agent.EffortID(choice.value)
		state.choiceOpen = false
	case fixFieldScope:
		state.input.ChangeScope = choice.value
		model.fixDialog.syncInput()
		state.choiceOpen = false
	case fixFieldWorkspace:
		state.input.DeliveryPlan.Workspace = fix.WorkspaceMode(choice.value)
		if state.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && state.input.DeliveryPlan.Git == fix.GitCommitCurrent {
			state.input.DeliveryPlan.Git = fix.GitLeaveUncommitted
			state.input.DeliveryPlan.Publish = fix.PublishLocal
		}
		model.fixDialog.syncInput()
		state.choiceOpen = false
		model.fixDialog.ensureCursorVisible()
	case fixFieldGit:
		state.input.DeliveryPlan.Git = fix.GitMode(choice.value)
		if state.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
			state.input.DeliveryPlan.Publish = fix.PublishLocal
		}
		model.fixDialog.syncInput()
		state.choiceOpen = false
		model.fixDialog.ensureCursorVisible()
	case fixFieldPublish:
		state.input.DeliveryPlan.Publish = fix.PublishMode(choice.value)
		model.fixDialog.syncInput()
		state.choiceOpen = false
		model.fixDialog.ensureCursorVisible()
	}
	return model, nil
}

func (model *Model) adjustFixField(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch state.cursor {
	case fixFieldTargetScore:
		return model.adjustFixScore(direction)
	case fixFieldProfile:
		return model.adjustFixProfile(direction)
	case fixFieldModel:
		state.input.Model = cycleAgentOption(state.input.Probe.Capabilities.Models, state.input.Model, direction)
	case fixFieldEffort:
		state.input.Effort = cycleAgentOption(state.input.Probe.Capabilities.Efforts, state.input.Effort, direction)
	case fixFieldScope:
		state.input.ChangeScope = fixCycleString([]string{"targets-only", "targets-and-tests", "repository"}, state.input.ChangeScope, direction)
		model.fixDialog.syncInput()
	}
	return model, nil
}

func (model *Model) adjustFixScore(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	state.input.TargetScore = max(0, state.input.TargetScore+float64(direction*10))
	if !model.fixDialog.syncInput() {
		return model, nil
	}
	model.fixTargetDesired = state.input.TargetScore
	if model.fixTargetSaving {
		return model, nil
	}
	return model, model.saveFixTargetPreference(state.input.Preferences)
}

func (model *Model) adjustFixProfile(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	profiles := state.input.Preferences.Profiles
	if len(profiles) <= 1 {
		return model, nil
	}
	current := 0
	for index := range profiles {
		if profiles[index].ID == state.input.Profile.ID {
			current = index
			break
		}
	}
	profile := profiles[fixCycleIndex(current, direction, len(profiles))].ID
	model.fixGeneration++
	state.generation = model.fixGeneration
	state.loading = true
	state.statusText = "Checking agent profile…"
	return model, model.loadFixCommand(state.targetPaths(), &profile, state.generation)
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
		model.fixDialog.input.Preferences = message.saved.Resolved
	}
	if model.fixTargetDesired != message.score {
		preferences := model.fixDialog.input.Preferences
		if message.err == nil {
			preferences = message.saved.Resolved
		}
		return model.saveFixTargetPreference(preferences)
	}
	return nil
}

func (model *Model) runFix() (tea.Model, tea.Cmd) {
	if !model.fixDialog.syncInput() {
		return model, nil
	}
	state := &model.fixDialog
	if !model.fixDialog.runnable() {
		state.errorText = "Run fix is unavailable: " + fixPreflightSummary(state.input)
		return model, nil
	}
	state.starting = true
	state.statusText = "Starting fix…"
	service, generation, input := model.fixService, state.generation, state.input
	return model, func() tea.Msg {
		jobID, err := service.Run(context.Background(), input)
		return fixStartedMsg{generation: generation, jobID: jobID, err: err}
	}
}

func (model *Model) handleFixStarted(message fixStartedMsg) {
	if message.generation != model.fixDialog.generation || !model.hasOverlay(OverlayFixForm) {
		return
	}
	model.fixDialog.starting = false
	if message.err != nil {
		model.fixDialog.errorText = message.err.Error()
		model.fixDialog.statusText = "Could not start fix · correct the reported error or retry"
		return
	}
	model.overlays.Pop()
	model.status = ""
	model.switchMainView(MainViewAgents)
	model.agents.FindQuery = ""
	model.agents.Selected = AgentRowID{JobID: message.jobID}
}

func (model *Model) openFixRemediationSettings() tea.Cmd {
	kind, ok := model.fixDialog.remediationSettingsKind()
	if !ok {
		model.fixDialog.statusText = "No settings remediation is available · resolve the diagnostic, then press R to recheck"
		return nil
	}
	diagnostic := model.fixDialog.errorText
	if diagnostic == "" && model.fixDialog.hasInput {
		diagnostic = fixPreflightSummary(model.fixDialog.input)
	}
	model.fixNotice = "Fix blocked: " + cleanAgentText(diagnostic)
	command := model.openConfigSettings(kind)
	model.configSettings.returnToFix = true
	model.overlays.Push(OverlayConfigSettings, OverlayCaller{
		MainView: MainViewFiles, Overlay: OverlayFixForm, Selected: model.fixDialog.target.String(),
	})
	return command
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
		return fixJobsMsg{jobs: snapshot.Jobs}
	}
}

func waitFixJobsCommand(service FixService, subscription fixapp.Subscription) tea.Cmd {
	return func() tea.Msg {
		err := subscription.Wait(context.Background())
		if err != nil {
			return fixJobsMsg{err: err}
		}
		snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
		return fixJobsMsg{jobs: snapshot.Jobs}
	}
}

func (model *Model) handleFixJobs(message fixJobsMsg) tea.Cmd {
	if message.err != nil {
		return model.fixUpdateError(message.err)
	}
	model.clearFixUpdateError()
	previousMonitorUpdate, previousLogUpdate := model.openFixSurfaceUpdates()
	model.agents.setPresentations(message.jobs, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	monitorCommand, logCommand := model.refreshOpenFixSurfaces(previousMonitorUpdate, previousLogUpdate)
	return model.nextFixUpdate(monitorCommand, logCommand)
}

func (model *Model) fixUpdateError(err error) tea.Cmd {
	model.fixNotice = "Fix updates unavailable: " + err.Error()
	model.fixUpdatesStale = true
	model.fixRetryGeneration++
	generation := model.fixRetryGeneration
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return fixRetrySubscriptionMsg{generation: generation} })
}

func (model *Model) clearFixUpdateError() {
	model.fixUpdatesStale = false
	if strings.HasPrefix(model.fixNotice, "Fix updates unavailable:") {
		model.fixNotice = ""
	}
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
		case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing, fix.PhaseRunning,
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
			jobID: job.ID, action: action, allowed: true,
		}
		model.overlays.Push(OverlayConfirmation, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: job.ID}.String()})
		return model, nil
	}
	model.jobCommand = jobCommandState{jobID: job.ID, action: action, pending: true}
	return model.executeSelectedJobAction(job.ID, action, false)
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
	return model.executeSelectedJobAction(model.cancelConfirmation.jobID, model.cancelConfirmation.action, true)
}

func jobActionPastTense(action fix.JobAction) string {
	if action == fix.ActionCancel {
		return "Cancel"
	}
	return "Action"
}
