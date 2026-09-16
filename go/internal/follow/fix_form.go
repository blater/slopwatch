package follow

import (
	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

type fixDialogFormAction uint8

const (
	fixDialogActionNone fixDialogFormAction = iota
	fixDialogActionClose
	fixDialogActionRefresh
	fixDialogActionRun
	fixDialogActionRemediate
	fixDialogActionScoreEditor
	fixDialogActionSaveTarget
	fixDialogActionReloadProfile
)

type fixDialogFormResult struct {
	action  fixDialogFormAction
	command tea.Cmd
	profile agent.ProfileID
}

// handleFormKey owns the fix form's state transitions. The model interprets
// the resulting action to start commands or change overlays.
func (state *fixDialogState) handleFormKey(key tea.KeyMsg) fixDialogFormResult {
	name := key.String()
	if state.branchEditing {
		return state.handleBranchKey(name, key)
	}
	if state.choiceOpen {
		choice, selected := state.handleChoiceKey(name)
		if selected {
			return state.applyChoice(choice)
		}
		return fixDialogFormResult{}
	}
	if state.starting {
		if name == "esc" || name == "q" {
			state.statusText = "Start in progress…"
		}
		return fixDialogFormResult{}
	}
	if name == "esc" || name == "q" {
		state.generation++
		return fixDialogFormResult{action: fixDialogActionClose}
	}
	if state.loading || !state.hasInput {
		if name == "R" {
			state.loading = true
			state.errorText = ""
			state.statusText = "Refreshing analysis…"
			return fixDialogFormResult{action: fixDialogActionRefresh}
		}
		if name == "s" && state.errorText != "" {
			return fixDialogFormResult{action: fixDialogActionRemediate}
		}
		return fixDialogFormResult{}
	}

	switch name {
	case "r":
		return fixDialogFormResult{action: fixDialogActionRun}
	case "R":
		state.loading = true
		state.errorText = ""
		state.statusText = "Refreshing analysis…"
		return fixDialogFormResult{action: fixDialogActionReloadProfile, profile: state.input.Profile.ID}
	case "s":
		return fixDialogFormResult{action: fixDialogActionRemediate}
	case "up", "k":
		state.moveCursor(-1)
	case "down", "j":
		state.moveCursor(1)
	case "left":
		if !fixChoiceField(state.cursor) {
			return state.adjustField(-1)
		}
	case "right":
		if !fixChoiceField(state.cursor) {
			return state.adjustField(1)
		}
	case "enter":
		return state.enterField()
	}
	return fixDialogFormResult{}
}

func (state *fixDialogState) handleBranchKey(name string, key tea.KeyMsg) fixDialogFormResult {
	switch name {
	case "enter":
		state.branchEditing = false
		state.branch.Blur()
		state.syncInput()
		state.branchOriginal = state.branch.Value()
	case "esc":
		state.branchEditing = false
		state.branch.Blur()
		state.branch.SetValue(state.branchOriginal)
	default:
		updated, command := state.branch.Update(key)
		state.branch = updated
		return fixDialogFormResult{command: command}
	}
	return fixDialogFormResult{}
}

func (state *fixDialogState) enterField() fixDialogFormResult {
	if fixChoiceField(state.cursor) {
		state.openChoice()
		return fixDialogFormResult{}
	}
	switch state.cursor {
	case fixFieldTargetScore:
		return fixDialogFormResult{action: fixDialogActionScoreEditor}
	case fixFieldBranch:
		state.branchOriginal = state.branch.Value()
		state.branchEditing = true
		state.branch.Focus()
	}
	return fixDialogFormResult{}
}

func (state *fixDialogState) applyChoice(choice fixDialogChoice) fixDialogFormResult {
	switch state.choiceField {
	case fixFieldFocus:
		id := fix.MetricID(choice.value)
		state.focus[id] = !state.focus[id]
		state.syncInput()
	case fixFieldProfile:
		profile := agent.ProfileID(choice.value)
		state.choiceOpen = false
		if profile != state.input.Profile.ID {
			state.loading = true
			state.statusText = "Checking agent profile…"
			return fixDialogFormResult{action: fixDialogActionReloadProfile, profile: profile}
		}
	case fixFieldModel:
		state.input.Model = agent.ModelID(choice.value)
		state.choiceOpen = false
	case fixFieldEffort:
		state.input.Effort = agent.EffortID(choice.value)
		state.choiceOpen = false
	case fixFieldScope:
		state.input.ChangeScope = choice.value
		state.syncInput()
		state.choiceOpen = false
	case fixFieldWorkspace:
		state.input.DeliveryPlan.Workspace = fix.WorkspaceMode(choice.value)
		if state.input.DeliveryPlan.Workspace == fix.WorkspaceWorktree && state.input.DeliveryPlan.Git == fix.GitCommitCurrent {
			state.input.DeliveryPlan.Git = fix.GitLeaveUncommitted
			state.input.DeliveryPlan.Publish = fix.PublishLocal
		}
		state.syncInput()
		state.choiceOpen = false
		state.ensureCursorVisible()
	case fixFieldGit:
		state.input.DeliveryPlan.Git = fix.GitMode(choice.value)
		if state.input.DeliveryPlan.Git == fix.GitLeaveUncommitted {
			state.input.DeliveryPlan.Publish = fix.PublishLocal
		}
		state.syncInput()
		state.choiceOpen = false
		state.ensureCursorVisible()
	case fixFieldPublish:
		state.input.DeliveryPlan.Publish = fix.PublishMode(choice.value)
		state.syncInput()
		state.choiceOpen = false
		state.ensureCursorVisible()
	}
	return fixDialogFormResult{}
}

func (state *fixDialogState) adjustField(direction int) fixDialogFormResult {
	switch state.cursor {
	case fixFieldTargetScore:
		state.input.TargetScore = max(0, state.input.TargetScore+float64(direction*10))
		if state.syncInput() {
			return fixDialogFormResult{action: fixDialogActionSaveTarget}
		}
	case fixFieldProfile:
		profiles := state.input.Preferences.Profiles
		if len(profiles) <= 1 {
			return fixDialogFormResult{}
		}
		current := 0
		for index := range profiles {
			if profiles[index].ID == state.input.Profile.ID {
				current = index
				break
			}
		}
		state.loading = true
		state.statusText = "Checking agent profile…"
		return fixDialogFormResult{action: fixDialogActionReloadProfile, profile: profiles[fixCycleIndex(current, direction, len(profiles))].ID}

	case fixFieldModel:
		state.input.Model = cycleAgentOption(state.input.Probe.Capabilities.Models, state.input.Model, direction)
	case fixFieldEffort:
		state.input.Effort = cycleAgentOption(state.input.Probe.Capabilities.Efforts, state.input.Effort, direction)
	case fixFieldScope:
		state.input.ChangeScope = fixCycleString([]string{"targets-only", "targets-and-tests", "repository"}, state.input.ChangeScope, direction)
		state.syncInput()
	}
	return fixDialogFormResult{}
}

func (model *Model) handleFixFormKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	result := model.fixDialog.handleFormKey(key)
	if result.command != nil {
		return model, result.command
	}
	switch result.action {
	case fixDialogActionClose:
		model.overlays.Pop()
	case fixDialogActionRefresh:
		return model, model.reloadFixForm(nil)
	case fixDialogActionRun:
		return model.runFix()
	case fixDialogActionRemediate:
		return model, model.openFixRemediationSettings()
	case fixDialogActionScoreEditor:
		return model, model.openFixTargetScoreEditor()
	case fixDialogActionSaveTarget:
		return model, model.targetScorePreference.request(model.fixDialog.input.TargetScore, model.fixDialog.input.Preferences, model.configStore, model.configWorkspace)
	case fixDialogActionReloadProfile:
		profile := result.profile
		return model, model.reloadFixForm(&profile)
	}
	return model, nil
}

func (model *Model) reloadFixForm(profile *agent.ProfileID) tea.Cmd {
	model.fixGeneration++
	model.fixDialog.generation = model.fixGeneration
	var delivery *fixapp.LoadDelivery
	if model.fixDialog.hasInput {
		delivery = &fixapp.LoadDelivery{Plan: model.fixDialog.input.DeliveryPlan, Branch: model.fixDialog.input.BranchName}
	}
	workspace := fixLoadWorkspace(model.fixWorkspace, model.options.Workspace)
	return loadFixCommand(model.fixService, workspace, model.fixDialog.targetPaths(), profile, delivery, model.fixDialog.generation)
}
