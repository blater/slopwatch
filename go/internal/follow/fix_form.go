package follow

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (model *Model) handleFixFormKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	state := &model.fixDialog
	if state.branchEditing {
		return model.handleFixBranchKey(name, key)
	}
	if state.choiceOpen {
		return model.handleFixChoiceKey(name)
	}
	if state.starting {
		return model.handleFixStartingKey(name)
	}
	if name == "esc" || name == "q" {
		model.fixDialog.generation++
		model.overlays.Pop()
		return model, nil
	}
	if state.loading || !state.hasInput {
		return model.handleFixLoadingKey(name)
	}
	return model.handleFixReadyKey(name)
}

func (model *Model) handleFixBranchKey(name string, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	switch name {
	case "enter":
		state.branchEditing = false
		state.branch.Blur()
		model.fixDialog.syncInput()
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

func (model *Model) handleFixStartingKey(name string) (tea.Model, tea.Cmd) {
	if name == "esc" || name == "q" {
		model.fixDialog.statusText = "Start in progress…"
	}
	return model, nil
}

func (model *Model) handleFixLoadingKey(name string) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	if name == "R" {
		model.fixGeneration++
		state.generation = model.fixGeneration
		state.loading = true
		state.errorText = ""
		state.statusText = "Refreshing analysis…"
		return model, model.loadFixCommand(state.targetPaths(), nil, state.generation)
	}
	if name == "s" && state.errorText != "" {
		return model, model.openFixRemediationSettings()
	}
	return model, nil
}

func (model *Model) handleFixReadyKey(name string) (tea.Model, tea.Cmd) {
	state := &model.fixDialog
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
		model.fixDialog.moveCursor(-1)
	case "down", "j":
		model.fixDialog.moveCursor(1)
	case "left":
		if !fixChoiceField(state.cursor) {
			return model.adjustFixField(-1)
		}
	case "right":
		if !fixChoiceField(state.cursor) {
			return model.adjustFixField(1)
		}
	case "enter":
		return model.handleFixReadyEnter()
	}
	return model, nil
}

func (model *Model) handleFixReadyEnter() (tea.Model, tea.Cmd) {
	state := &model.fixDialog
	if fixChoiceField(state.cursor) {
		model.fixDialog.openChoice()
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
	return model, nil
}
