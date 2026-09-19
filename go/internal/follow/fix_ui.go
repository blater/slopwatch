package follow

import (
	"context"
	"errors"
	"fmt"

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
		showRuntimeError(model, fmt.Errorf("Fix unavailable for this row: %w", err))
		return nil
	}
	path := targets[0]
	if len(targets) == 1 {
		if job, ok := existingFixForTarget(model.agents.Jobs, path); ok {
			model.switchMainView(MainViewAgents)
			model.agents.FindQuery = ""
			model.agents.Selected = AgentRowID{JobID: job.ID}
			model.agents.ensureVisible(makeAgentLayout(model.width, model.height, bodyHeight(model.mainView, model.height)))
			model.fixNotice = "Opened existing fix for " + path.String()
			return nil
		}
	}
	if model.fixService == nil {
		reason := "Fix unavailable: configure an agent service in Settings"
		if model.options.FixUnavailableReason != "" {
			reason = "Fix unavailable: " + model.options.FixUnavailableReason
		}
		showRuntimeError(model, errors.New(reason))
		return nil
	}
	model.runtime.fixGeneration++
	branch := textinput.New()
	branch.Prompt = ""
	style.ApplyTextInputStyle(&branch, false)
	score := textinput.New()
	score.Prompt = ""
	style.ApplyTextInputStyle(&score, false)
	model.fixDialog = fixDialogState{
		generation: model.runtime.fixGeneration, target: path, targets: append([]fix.RepoPath(nil), targets...), loading: true, focus: map[fix.MetricID]bool{}, score: score, branch: branch,
		statusText: "Preparing analysis of " + markedFilesLabel(len(targets)) + "…",
	}
	model.overlays.Push(OverlayFixForm, OverlayCaller{MainView: MainViewFiles, Selected: model.files.Selected})
	workspace := fixLoadWorkspace(model.fixWorkspace, model.options.Workspace)
	return loadFixCommand(model.fixService, workspace, targets, nil, nil, model.runtime.fixGeneration)
}

func fixLoadWorkspace(workspace fix.WorkspaceIdentity, fallback string) fix.WorkspaceIdentity {
	if workspace.RepositoryRoot == "" {
		workspace.RepositoryRoot = fallback
		workspace.AnalysisRoot = fallback
	}
	return workspace
}

func loadFixCommand(service FixService, workspace fix.WorkspaceIdentity, paths []fix.RepoPath, profile *agent.ProfileID, selectedDelivery *fixapp.LoadDelivery, generation uint64) tea.Cmd {
	return func() tea.Msg {
		overrides := appconfig.SessionOverrides{Profile: profile}
		input, err := service.LoadFix(context.Background(), fixapp.LoadRequest{
			Workspace: workspace, Targets: append([]fix.RepoPath(nil), paths...), Overrides: overrides, Delivery: selectedDelivery,
		})
		return fixLoadedMsg{generation: generation, input: input, err: err}
	}
}

func (model *Model) handleFixLoaded(message fixLoadedMsg) {
	if message.generation != model.fixDialog.generation || !overlayPresent(model.overlays, OverlayFixForm) {
		return
	}
	model.fixDialog.applyLoaded(message)
}

func (model *Model) openFixTargetScoreEditor() tea.Cmd {
	model.overlays.Push(OverlayTargetScoreEditor, OverlayCaller{MainView: model.mainView, Overlay: OverlayFixForm, Selected: model.mainSelection()})
	return model.fixDialog.beginTargetScoreEditor()
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
	if save := model.runtime.targetScorePreference.request(result.value, model.fixDialog.input.Preferences, model.configStore, model.configWorkspace); save != nil {
		return model, save
	}
	return model, command
}

type fixDialogChoice struct {
	value    string
	label    string
	selected bool
	disabled bool
}

func (model *Model) handleFixTargetPreferenceSaved(message fixTargetPreferenceSavedMsg) tea.Cmd {
	outcome := model.runtime.targetScorePreference.complete(message, model.fixDialog.input.Preferences, model.configStore, model.configWorkspace)
	if message.err != nil {
		showRuntimeError(model, fmt.Errorf("Target score preference was not saved: %w", message.err))
	} else {
		model.fixDialog.input.Preferences = outcome.preferences
	}
	return outcome.command
}

func (model *Model) runFix() (tea.Model, tea.Cmd) {
	input, ok := model.fixDialog.prepareRun()
	if !ok {
		return model, nil
	}
	return model, runFixCommand(model.fixService, input, model.fixDialog.generation)
}

func runFixCommand(service FixService, input fixapp.FixInput, generation uint64) tea.Cmd {
	return func() tea.Msg {
		jobID, err := service.Run(context.Background(), input)
		return fixStartedMsg{generation: generation, jobID: jobID, err: err}
	}
}

func (model *Model) handleFixStarted(message fixStartedMsg) {
	if message.generation != model.fixDialog.generation || !overlayPresent(model.overlays, OverlayFixForm) {
		return
	}
	if !model.fixDialog.applyStarted(message) {
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

func overlayPresent(stack OverlayStack, kind OverlayKind) bool {
	for _, frame := range stack.frames {
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

func (model *Model) handleFixJobs(message fixJobsMsg) tea.Cmd {
	if message.err != nil {
		return model.fixUpdateError(message.err)
	}
	model.fixNotice = model.fixUpdates.clearError(model.fixNotice)
	model.runtime.fixErrorSummary = ""
	showJobErrors(model, message.jobs)
	previousMonitorUpdate, previousLogUpdate := model.openFixSurfaceUpdates()
	model.agents.setPresentations(message.jobs, makeAgentLayout(model.width, model.height, bodyHeight(model.mainView, model.height)))
	monitorCommand, logCommand := model.refreshOpenFixSurfaces(previousMonitorUpdate, previousLogUpdate)
	return model.fixUpdates.next(model.fixService, monitorCommand, logCommand)
}

func (model *Model) fixUpdateError(err error) tea.Cmd {
	var retry tea.Cmd
	_, retry = model.fixUpdates.markUnavailable(err)
	if model.runtime.fixErrorSummary != err.Error() {
		model.runtime.fixErrorSummary = err.Error()
		showRuntimeError(model, fmt.Errorf("Fix updates unavailable: %w", err))
	}
	return retry
}

func (model *Model) requestQuit() (tea.Model, tea.Cmd) {
	active := activeFixJobs(model.agents.Jobs)
	if len(active) == 0 || model.fixService == nil {
		return model, tea.Quit
	}
	model.runtime.shutdown = shutdownState{active: len(active)}
	model.overlays.Push(OverlayShutdown, OverlayCaller{MainView: model.mainView, Selected: model.mainSelection()})
	return model, nil
}

func activeFixJobs(jobs []fix.JobPresentation) []fix.JobPresentation {
	result := make([]fix.JobPresentation, 0)
	for _, job := range jobs {
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
	outcome := model.runtime.shutdown.handleKey(key, model.fixService)
	if outcome.close {
		model.overlays.Pop()
		// Errors received while shutdown confirmation was on top are retained
		// until the confirmation closes, then become the next modal.
		showStoredRuntimeError(model)
	}
	return model, outcome.command
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
	job, ok := jobByID(model.agents.Jobs, jobID)
	if !ok {
		showRuntimeError(model, errors.New("Selected job is no longer available"))
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
		showRuntimeError(model, errors.New(jobActionLabel(choices[0])+" is unavailable for this job"))
		return model, nil
	}
	if jobActionRequiresConfirmation(action) {
		model.runtime.jobActions.beginConfirmation(job.ID, action)
		model.overlays.Push(OverlayConfirmation, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: job.ID}.String()})
		return model, nil
	}
	return model.executeSelectedJobAction(job.ID, action, false)
}

func jobByID(jobs []fix.JobPresentation, id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func jobActionRequiresConfirmation(action fix.JobAction) bool {
	return action == fix.ActionCancel
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
	if (key.String() == "esc" || key.String() == "q") && !model.runtime.jobActions.confirmation.pending {
		model.overlays.Pop()
		return model, nil
	}
	if key.String() != "enter" || model.runtime.jobActions.confirmation.pending || !model.runtime.jobActions.confirmation.allowed {
		return model, nil
	}
	return model.executeSelectedJobAction(model.runtime.jobActions.confirmation.jobID, model.runtime.jobActions.confirmation.action, true)
}

func jobActionPastTense(action fix.JobAction) string {
	if action == fix.ActionCancel {
		return "Cancel"
	}
	return "Action"
}
