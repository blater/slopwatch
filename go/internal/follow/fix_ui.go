package follow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
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
	targets, err := model.selectedFixTargets()
	if err != nil {
		model.status = "Fix unavailable for this row: " + err.Error()
		return nil
	}
	path := targets[0]
	if len(targets) == 1 {
		if job, ok := model.existingFixForTarget(path); ok {
			model.switchMainView(MainViewAgents)
			model.agents.FindQuery = ""
			model.agents.Selected = AgentRowID{JobID: job.ID}
			model.ensureAgentVisible()
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
	model.overlays.Push(OverlayFixForm, OverlayCaller{MainView: MainViewFiles, Selected: model.selected})
	return model.loadFixCommand(targets, nil, model.fixGeneration)
}

func (model Model) selectedFixTargets() ([]fix.RepoPath, error) {
	paths := model.markedFilePaths()
	if len(paths) == 0 {
		paths = []string{model.selected}
	}
	targets := make([]fix.RepoPath, 0, len(paths))
	for _, path := range paths {
		target, err := model.selectedRepoPath(path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
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
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing:
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

func (state fixDialogState) targetPaths() []fix.RepoPath {
	if len(state.targets) > 0 {
		return append([]fix.RepoPath(nil), state.targets...)
	}
	if state.target != "" {
		return []fix.RepoPath{state.target}
	}
	return nil
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
	model.ensureFixCursorVisible()
}

func availableFixMetrics(input fixapp.FixInput) []fix.MetricID {
	targets := input.Baseline.Contract.Targets
	if len(targets) == 0 {
		return []fix.MetricID{fix.MetricScore}
	}
	seen := map[fix.MetricID]int{}
	for _, target := range targets {
		for id, metric := range target.Metrics {
			if metric.Complete {
				seen[id]++
			}
		}
	}
	result := make([]fix.MetricID, 0, len(seen)+1)
	for id, count := range seen {
		if count == len(targets) {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return append([]fix.MetricID{fix.MetricScore}, result...)
}

func (state fixDialogState) fixFormValues() fixapp.FormValues {
	goals := make([]fix.MetricGoal, 0, len(state.focus))
	for _, id := range state.metrics {
		if !state.focus[id] {
			continue
		}
		if id == fix.MetricScore {
			goals = append(goals, fix.MetricGoal{Metric: id, Maximum: state.input.TargetScore})
			continue
		}
		maximum := 0.0
		found := false
		for _, target := range state.input.Baseline.Contract.Targets {
			if metric, ok := target.Metrics[id]; ok && metric.Complete && (!found || metric.Value > maximum) {
				maximum, found = metric.Value, true
			}
		}
		if found {
			goals = append(goals, fix.MetricGoal{Metric: id, Maximum: maximum})
		}
	}
	return fixapp.FormValues{
		TargetScore: state.input.TargetScore,
		Focus:       goals, ChangeScope: state.input.ChangeScope,
		DeliveryPlan: state.input.DeliveryPlan, BranchName: state.branch.Value(),
	}
}

func (model *Model) syncFixInput() bool {
	revised, err := fixapp.ApplyFormValues(model.fixDialog.input, model.fixDialog.fixFormValues())
	if err != nil {
		model.fixDialog.errorText = err.Error()
		return false
	}
	model.fixDialog.input = revised
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
			model.syncFixInput()
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
	if state.choiceOpen {
		return model.handleFixChoiceKey(name)
	}
	if state.starting && (name == "esc" || name == "q") {
		state.statusText = "Start in progress…"
		return model, nil
	}
	if name == "esc" || name == "q" {
		model.fixDialog.generation++
		model.overlays.Pop()
		return model, nil
	}
	if state.loading || state.starting || !state.hasInput {
		if name == "R" && !state.loading && !state.starting {
			model.fixGeneration++
			state.generation = model.fixGeneration
			state.loading = true
			state.errorText = ""
			state.statusText = "Refreshing analysis…"
			return model, model.loadFixCommand(state.targetPaths(), nil, state.generation)
		}
		if name == "s" && !state.starting && state.errorText != "" {
			return model, model.openFixRemediationSettings()
		}
		return model, nil
	}
	switch name {
	case "r":
		return model.runFix()
	case "R":
		model.fixGeneration++
		state.generation = model.fixGeneration
		state.loading = true
		state.errorText = ""
		state.statusText = "Refreshing analysis…"
		profile := state.input.Profile.ID
		return model, model.loadFixCommand(state.targetPaths(), &profile, state.generation)
	case "s":
		return model, model.openFixRemediationSettings()
	case "up", "k":
		model.moveFixCursor(-1)
	case "down", "j":
		model.moveFixCursor(1)
	case "left":
		if fixChoiceField(state.cursor) {
			return model, nil
		}
		return model.adjustFixField(-1)
	case "right":
		if fixChoiceField(state.cursor) {
			return model, nil
		}
		return model.adjustFixField(1)
	case "enter":
		if fixChoiceField(state.cursor) {
			model.openFixChoice()
			return model, nil
		}
		switch state.cursor {
		case fixFieldTargetScore:
			return model, model.openFixTargetScoreEditor()
		case fixFieldBranch:
			state.branchOriginal = state.branch.Value()
			state.branchEditing = true
			state.branch.Focus()
		}
	}
	return model, nil
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
	state := &model.fixDialog
	switch key.String() {
	case "enter":
		value, err := strconv.ParseFloat(strings.TrimSpace(state.score.Value()), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			state.scoreError = "Enter a non-negative number"
			return model, nil
		}
		previous := state.input.TargetScore
		state.input.TargetScore = value
		if !model.syncFixInput() {
			state.input.TargetScore = previous
			state.scoreError = cleanAgentText(state.errorText)
			state.errorText = ""
			return model, nil
		}
		state.scoreEditing = false
		state.score.Blur()
		state.scoreOriginal = value
		state.scoreError = ""
		model.overlays.Pop()
		model.fixTargetDesired = value
		if !model.fixTargetSaving {
			return model, model.saveFixTargetPreference(state.input.Preferences)
		}
		return model, nil
	case "esc", "escape":
		state.scoreEditing = false
		state.score.Blur()
		state.score.SetValue(formatTargetScore(state.scoreOriginal))
		state.scoreError = ""
		model.overlays.Pop()
		return model, nil
	default:
		updated, command := state.score.Update(key)
		state.score = updated
		state.scoreError = ""
		return model, command
	}
}

type fixDialogChoice struct {
	value    string
	label    string
	selected bool
	disabled bool
}

func fixChoiceField(field int) bool {
	switch field {
	case fixFieldFocus, fixFieldProfile, fixFieldModel, fixFieldEffort, fixFieldScope, fixFieldWorkspace, fixFieldGit, fixFieldPublish:
		return true
	default:
		return false
	}
}

func (model Model) fixChoices(field int) []fixDialogChoice {
	state := model.fixDialog
	switch field {
	case fixFieldFocus:
		choices := make([]fixDialogChoice, 0, len(state.metrics))
		for _, id := range state.metrics {
			choices = append(choices, fixDialogChoice{
				value: string(id), label: fixMetricLabel(id), selected: state.focus[id],
			})
		}
		return choices
	case fixFieldProfile:
		choices := make([]fixDialogChoice, 0, len(state.input.Preferences.Profiles))
		for _, profile := range state.input.Preferences.Profiles {
			choices = append(choices, fixDialogChoice{
				value: string(profile.ID), label: agentProfileChoiceLabel(profile), selected: profile.ID == state.input.Profile.ID,
			})
		}
		return choices
	case fixFieldModel:
		choices := make([]fixDialogChoice, 0, len(state.input.Probe.Capabilities.Models))
		for _, option := range state.input.Probe.Capabilities.Models {
			choices = append(choices, fixDialogChoice{value: string(option.ID), label: option.Label, selected: option.ID == state.input.Model})
			if choices[len(choices)-1].label == "" {
				choices[len(choices)-1].label = string(option.ID)
			}
		}
		return choices
	case fixFieldEffort:
		choices := make([]fixDialogChoice, 0, len(state.input.Probe.Capabilities.Efforts))
		for _, option := range state.input.Probe.Capabilities.Efforts {
			choices = append(choices, fixDialogChoice{value: string(option.ID), label: option.Label, selected: option.ID == state.input.Effort})
			if choices[len(choices)-1].label == "" {
				choices[len(choices)-1].label = string(option.ID)
			}
		}
		return choices
	case fixFieldScope:
		values := []string{"targets-only", "targets-and-tests", "repository"}
		choices := make([]fixDialogChoice, 0, len(values))
		for _, value := range values {
			choices = append(choices, fixDialogChoice{value: value, label: changeScopeLabel(value), selected: value == state.input.ChangeScope})
		}
		return choices
	case fixFieldWorkspace:
		return []fixDialogChoice{
			{value: string(fix.WorkspaceCurrent), label: workspaceModeLabel(fix.WorkspaceCurrent), selected: state.input.DeliveryPlan.Workspace == fix.WorkspaceCurrent},
			{value: string(fix.WorkspaceWorktree), label: workspaceModeLabel(fix.WorkspaceWorktree), selected: state.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree, disabled: state.input.Workspace.GitCommonDir == ""},
		}
	case fixFieldGit:
		values := []fix.GitMode{fix.GitLeaveUncommitted, fix.GitCommitCurrent, fix.GitCommitNewBranch}
		choices := make([]fixDialogChoice, 0, len(values))
		for _, value := range values {
			disabled := state.input.Workspace.GitCommonDir == "" && value != fix.GitLeaveUncommitted
			if state.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && value == fix.GitCommitCurrent {
				disabled = true
			}
			choices = append(choices, fixDialogChoice{value: string(value), label: gitModeLabel(value), selected: value == state.input.DeliveryPlan.Git, disabled: disabled})
		}
		return choices
	case fixFieldPublish:
		values := []fix.PublishMode{fix.PublishLocal, fix.PublishPush, fix.PublishPullRequest}
		choices := make([]fixDialogChoice, 0, len(values))
		for _, value := range values {
			choices = append(choices, fixDialogChoice{value: string(value), label: publishModeLabel(value), selected: value == state.input.DeliveryPlan.Publish})
		}
		return choices
	default:
		return nil
	}
}

func (model *Model) openFixChoice() {
	state := &model.fixDialog
	choices := model.fixChoices(state.cursor)
	if len(choices) == 0 {
		return
	}
	state.choiceOpen = true
	state.choiceField = state.cursor
	state.choiceCursor = 0
	for index, choice := range choices {
		if !choice.disabled {
			state.choiceCursor = index
			break
		}
	}
	for index, choice := range choices {
		if choice.selected && !choice.disabled {
			state.choiceCursor = index
			break
		}
	}
}

func (model *Model) handleFixChoiceKey(name string) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	choices := model.fixChoices(state.choiceField)
	if len(choices) == 0 {
		state.choiceOpen = false
		return model, nil
	}
	switch name {
	case "esc", "escape", "q":
		state.choiceOpen = false
	case "up", "k":
		state.choiceCursor = max(0, state.choiceCursor-1)
	case "down", "j":
		state.choiceCursor = min(len(choices)-1, state.choiceCursor+1)
	default:
		if !isToggleKey(name) {
			return model, nil
		}
		choice := choices[state.choiceCursor]
		if choice.disabled {
			return model, nil
		}
		switch state.choiceField {
		case fixFieldFocus:
			id := fix.MetricID(choice.value)
			state.focus[id] = !state.focus[id]
			model.syncFixInput()
		case fixFieldProfile:
			state.choiceOpen = false
			profile := agent.ProfileID(choice.value)
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
			model.syncFixInput()
			state.choiceOpen = false
		case fixFieldWorkspace:
			state.input.DeliveryPlan.Workspace = fix.WorkspaceMode(choice.value)
			if state.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && state.input.DeliveryPlan.Git == fix.GitCommitCurrent {
				state.input.DeliveryPlan.Git = fix.GitLeaveUncommitted
				state.input.DeliveryPlan.Publish = fix.PublishLocal
			}
			model.syncFixInput()
			state.choiceOpen = false
			model.ensureFixCursorVisible()
		case fixFieldGit:
			state.input.DeliveryPlan.Git = fix.GitMode(choice.value)
			if state.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
				state.input.DeliveryPlan.Publish = fix.PublishLocal
			}
			model.syncFixInput()
			state.choiceOpen = false
			model.ensureFixCursorVisible()
		case fixFieldPublish:
			state.input.DeliveryPlan.Publish = fix.PublishMode(choice.value)
			model.syncFixInput()
			state.choiceOpen = false
			model.ensureFixCursorVisible()
		}
	}
	return model, nil
}

func (model *Model) adjustFixField(direction int) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch state.cursor {
	case fixFieldTargetScore:
		state.input.TargetScore = max(0, state.input.TargetScore+float64(direction*10))
		if model.syncFixInput() {
			model.fixTargetDesired = state.input.TargetScore
			if !model.fixTargetSaving {
				return model, model.saveFixTargetPreference(state.input.Preferences)
			}
		}
	case fixFieldProfile:
		profiles := state.input.Preferences.Profiles
		if len(profiles) > 1 {
			current := 0
			for index := range profiles {
				if profiles[index].ID == state.input.Profile.ID {
					current = index
				}
			}
			profile := profiles[fixCycleIndex(current, direction, len(profiles))].ID
			model.fixGeneration++
			state.generation = model.fixGeneration
			state.loading = true
			state.statusText = "Checking agent profile…"
			return model, model.loadFixCommand(state.targetPaths(), &profile, state.generation)
		}
	case fixFieldModel:
		state.input.Model = cycleAgentOption(state.input.Probe.Capabilities.Models, state.input.Model, direction)
	case fixFieldEffort:
		state.input.Effort = cycleAgentOption(state.input.Probe.Capabilities.Efforts, state.input.Effort, direction)
	case fixFieldScope:
		state.input.ChangeScope = fixCycleString([]string{"targets-only", "targets-and-tests", "repository"}, state.input.ChangeScope, direction)
		model.syncFixInput()
	}
	return model, nil
}

func formatTargetScore(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
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

func (model *Model) runFix() (tea.Model, tea.Cmd) {
	if !model.syncFixInput() {
		return model, nil
	}
	state := &model.fixDialog
	if !model.fixDialogRunnable() {
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

func (model Model) fixDialogRunnable() bool {
	return model.fixDialog.hasInput
}

func (model *Model) openFixRemediationSettings() tea.Cmd {
	kind, ok := model.fixRemediationSettingsKind()
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

func (model Model) fixRemediationSettingsKind() (configSettingsKind, bool) {
	state := model.fixDialog
	if state.hasInput {
		input := state.input
		if input.Probe.State == agent.ProbeUnauthenticated || input.Probe.State == agent.ProbeUnavailable || input.Probe.State == agent.ProbeIncompatible {
			return configAgents, true
		}
		if input.Probe.State == agent.ProbeDegraded || !input.Probe.Capabilities.Isolation.EligibleForMutation() {
			return configAgents, true
		}
		if !fixOptionContains(input.Probe.Capabilities.Models, input.Model) ||
			!fixOptionContains(input.Probe.Capabilities.Efforts, input.Effort) {
			return configFix, true
		}
		if strings.TrimSpace(input.BranchName) == "" {
			return configDelivery, true
		}
	}
	message := strings.ToLower(state.errorText)
	switch {
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
	var logCommand tea.Cmd
	previousMonitorUpdate := time.Time{}
	if model.hasOverlay(OverlayJobMonitor) {
		if job, ok := model.agentJobByID(model.jobMonitor.jobID); ok {
			previousMonitorUpdate = job.UpdatedAt
		}
	}
	previousLogUpdate := time.Time{}
	if model.hasOverlay(OverlayJobLog) {
		if job, ok := model.agentJobByID(model.jobReader.jobID); ok {
			previousLogUpdate = job.UpdatedAt
		}
	}
	model.setAgentPresentations(message.jobs)
	if model.hasOverlay(OverlayJobMonitor) {
		if job, ok := model.agentJobByID(model.jobMonitor.jobID); ok {
			model.jobMonitor.job = job
			if !job.UpdatedAt.Equal(previousMonitorUpdate) {
				monitorCommand = model.beginJobMonitorRefresh()
			}
		}
	}
	if model.hasOverlay(OverlayJobLog) {
		if job, ok := model.agentJobByID(model.jobReader.jobID); ok && !job.UpdatedAt.Equal(previousLogUpdate) {
			logCommand = model.beginJobLogRefresh()
		}
	}
	if model.fixService == nil || model.fixSubscription == nil {
		return tea.Batch(monitorCommand, logCommand)
	}
	waitCommand := waitFixJobsCommand(model.fixService, model.fixSubscription)
	return tea.Batch(waitCommand, monitorCommand, logCommand)
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
	model.setAgentPresentations(snapshot.Jobs)
	model.fixUpdatesStale = false
	model.fixNotice = "Fix updates restored"
	return waitFixJobsCommand(model.fixService, model.fixSubscription)
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
	model.jobReader = jobReaderState{
		generation: model.fixGeneration,
		kind:       kind,
		jobID:      jobID,
		path:       path,
		loading:    true,
		refreshing: true,
		follow:     kind == OverlayJobLog,
	}
	model.overlays.Push(kind, OverlayCaller{MainView: MainViewAgents, Overlay: OverlayJobMonitor, Selected: AgentRowID{JobID: jobID, Path: path}.String()})
	return model.loadJobReaderCommand(kind, jobID, path, model.fixGeneration, 0, false)
}

func (model *Model) loadJobReaderCommand(kind OverlayKind, jobID fix.JobID, path fix.RepoPath, generation uint64, cursor fixapp.LogCursor, increment bool) tea.Cmd {
	service := model.fixService
	return func() tea.Msg {
		lines := []string{}
		logCursor := cursor
		truncated := false
		var err error
		switch kind {
		case OverlayJobLog:
			var page fixapp.LogPage
			page, err = loadTranscript(context.Background(), service, jobID, cursor)
			logCursor = page.Next
			for _, entry := range page.Entries {
				if entry.Text != "" {
					lines = append(lines, jobLogDisplayLine(cleanAgentText(entry.Text)))
					continue
				}
				at := entry.At.Format("15:04:05")
				lines = append(lines, fmt.Sprintf("%s  %-16s %s", at, entry.Kind, cleanAgentText(entry.Summary)))
			}
			if !increment {
				if job, ok := service.Job(jobID); ok && agentJobFinished(job.Phase) {
					summary := strings.Join(nonemptyStrings(agentPhaseText(job), agentActivityText(job)), " · ")
					lines = append(lines, fmt.Sprintf("%s  %-16s %s", job.UpdatedAt.Format("15:04:05"), "result", summary))
				}
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
		return jobReaderMsg{generation: generation, kind: kind, lines: lines, logCursor: logCursor, increment: increment, truncated: truncated, err: err}
	}
}

func jobLogDisplayLine(line string) string {
	for _, prefix := range []string{"Started: ", "Finished: "} {
		if strings.HasPrefix(line, prefix) {
			return prefix + wholeSecondTimestamp(strings.TrimPrefix(line, prefix))
		}
	}
	if end := strings.IndexByte(line, ' '); end > 0 {
		return wholeSecondTimestamp(line[:end]) + line[end:]
	}
	return line
}

func wholeSecondTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Format(time.RFC3339)
}

func (model *Model) beginJobLogRefresh() tea.Cmd {
	if model.jobReader.kind != OverlayJobLog || !model.hasOverlay(OverlayJobLog) || model.fixService == nil {
		return nil
	}
	if model.jobReader.refreshing {
		model.jobReader.pending = true
		return nil
	}
	model.fixGeneration++
	model.jobReader.generation = model.fixGeneration
	model.jobReader.refreshing = true
	return model.loadJobReaderCommand(OverlayJobLog, model.jobReader.jobID, "", model.fixGeneration, model.jobReader.logCursor, true)
}

func loadTranscript(ctx context.Context, service FixService, jobID fix.JobID, cursor fixapp.LogCursor) (fixapp.LogPage, error) {
	result := fixapp.LogPage{}
	for {
		page, err := service.Transcript(ctx, jobID, cursor, 500)
		if err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, page.Entries...)
		result.Next = page.Next
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

func (model *Model) handleJobReader(message jobReaderMsg) tea.Cmd {
	if message.generation != model.jobReader.generation || message.kind != model.jobReader.kind || !model.hasOverlay(message.kind) {
		return nil
	}
	model.jobReader.loading = false
	model.jobReader.refreshing = false
	if message.err != nil {
		model.jobReader.errorText = message.err.Error()
	} else {
		model.jobReader.errorText = ""
		if message.increment {
			model.jobReader.lines = append(model.jobReader.lines, message.lines...)
		} else {
			model.jobReader.lines = append([]string(nil), message.lines...)
		}
		model.jobReader.logCursor = message.logCursor
		model.jobReader.truncated = message.truncated
	}
	model.clampJobReaderPosition()
	if model.jobReader.pending {
		model.jobReader.pending = false
		return model.beginJobLogRefresh()
	}
	return nil
}

func (model *Model) handleJobReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := model.jobReaderPageSize()
	maximum := model.jobReaderMaxOffset()
	switch key.String() {
	case "esc", "q":
		model.overlays.Pop()
		model.jobReader = jobReaderState{}
	case "up", "k":
		model.jobReader.offset = max(0, model.jobReader.offset-1)
		model.jobReader.follow = false
	case "down", "j":
		model.jobReader.offset = min(maximum, model.jobReader.offset+1)
		model.jobReader.follow = model.jobReader.offset == maximum
	case "pgup", "ctrl+b":
		model.jobReader.offset = max(0, model.jobReader.offset-page)
		model.jobReader.follow = false
	case "pgdown", "ctrl+f":
		model.jobReader.offset = min(maximum, model.jobReader.offset+page)
		model.jobReader.follow = model.jobReader.offset == maximum
	case "home", "g":
		model.jobReader.offset = 0
		model.jobReader.follow = false
	case "end", "G":
		model.jobReader.offset = maximum
		model.jobReader.follow = model.jobReader.kind == OverlayJobLog
	case "left", "h":
		model.jobReader.horizontal = max(0, model.jobReader.horizontal-pathScrollStep)
	case "right", "l":
		model.jobReader.horizontal = min(model.jobReaderMaxHorizontalOffset(), model.jobReader.horizontal+pathScrollStep)
	case "r":
		if model.jobReader.kind == OverlayJobLog {
			return model, model.beginJobLogRefresh()
		}
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

func (model *Model) executeSelectedJobAction(jobID fix.JobID, action fix.JobAction, confirmation bool) (tea.Model, tea.Cmd) {
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
	command := fix.JobCommand{RequestID: requestID, JobID: jobID, Action: action}
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
			model.cancelConfirmation.allowed = containsFixAction(job.AllowedActions, message.action)
			if model.cancelConfirmation.allowed {
				model.cancelConfirmation.errorText = errorText
			} else {
				model.cancelConfirmation.errorText = errorText + "; this job can no longer be canceled"
			}
		} else {
			if containsFixAction(job.AllowedActions, message.action) {
				model.fixNotice = errorText
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
