package follow

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
	state := &model.configSettings
	switch key.String() {
	case "ctrl+s":
		value := strings.TrimSpace(cleanEditorText(state.prompt.Value()))
		if value == "" {
			state.promptError = "Agent prompt cannot be empty"
			resizeMasterPromptTextBox(&model.configSettings, model.width, model.height)
			return model, nil
		}
		state.promptError = ""
		state.working.Fix.PromptTemplate = value
		state.promptOriginal = value
		state.prompt.Blur()
		state.dirty = true
		model.overlays.Pop()
		return model, model.saveConfigSettings()
	case "esc", "escape":
		state.prompt.SetValue(state.promptOriginal)
		state.promptError = ""
		state.prompt.Blur()
		model.overlays.Pop()
		return model, nil
	default:
		updated, command := state.prompt.Update(key)
		state.prompt = updated
		state.promptError = ""
		resizeMasterPromptTextBox(&model.configSettings, model.width, model.height)
		return model, command
	}
}
