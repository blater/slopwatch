package follow

import tea "github.com/charmbracelet/bubbletea"

func (model *Model) handleConfigSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	action := state.handleKey(key, model.profileCatalog, configSettingsRowsForState(*state))
	if action.kind == configKeyHandleChoice {
		return model.applyConfigKeyAction(state.handleChoiceKey(key.String()))
	}
	return model.applyConfigKeyAction(action)
}

func (model *Model) applyConfigKeyAction(action configKeyAction) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	switch action.kind {
	case configKeyClose:
		return model, model.closeConfigSettings()
	case configKeySave:
		return model, model.saveConfigSettings()
	case configKeyTestProfile:
		return model, state.testSelectedProfileCommand(model.profileProber)
	case configKeyOpenChoice:
		state.openChoice()
	case configKeyOpenPrompt:
		resizeMasterPromptTextBox(state, model.width, model.height)
		model.overlays.Push(OverlayPromptEditor, OverlayCaller{MainView: model.mainView, Overlay: OverlayConfigSettings, Selected: model.mainSelection()})
	case configKeyOpenProvider:
		return model, model.openSelectedAgentProvider()
	case configKeyAdjust:
		return model, model.adjustConfigSetting(action.direction)
	}
	return model, action.command
}

func (model *Model) handleMasterPromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	result := model.configSettings.handlePromptKey(key, model.width, model.height)
	if result.close {
		model.overlays.Pop()
	}
	if result.save {
		return model, model.saveConfigSettings()
	}
	return model, result.command
}
