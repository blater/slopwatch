package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
)

func (state *configSettingsState) handleAgentKey(key tea.KeyMsg, catalog agent.ProfileCatalog) configKeyAction {
	if state.profileEditing {
		if index := state.selectedProfileIndex(); index >= 0 && state.probing[state.working.Profiles[index].ID] && key.String() != "esc" && key.String() != "escape" {
			return configKeyAction{}
		}
		switch key.String() {
		case "esc", "escape":
			state.rollbackPendingAgentEdit()
			state.profileEditing = false
			state.pendingActive = ""
			state.cursor = state.providerCursor
			state.status = ""
		case "up", "k":
			state.profileCursor = max(0, state.profileCursor-1)
		case "down", "j":
			state.profileCursor = min(max(0, state.profileFieldCount(catalog)-1), state.profileCursor+1)
		case "left", "h", "-":
			if state.adjustProfileChoice(catalog, -1) {
				return configKeyAction{kind: configKeyTestProfile}
			}
		case "right", "l", "+", "=", " ":
			if state.adjustProfileChoice(catalog, 1) {
				return configKeyAction{kind: configKeyTestProfile}
			}
		case "enter":
			if state.profileFieldCount(catalog) > 0 {
				if !state.adjustProfileChoice(catalog, 1) {
					state.beginText(catalog)
					return configKeyAction{}
				}
				return configKeyAction{kind: configKeyTestProfile}
			}
			return configKeyAction{kind: configKeyTestProfile}
		}
		return configKeyAction{}
	}
	switch key.String() {
	case "esc", "escape", "q":
		return configKeyAction{kind: configKeyClose}
	case "up", "k":
		state.cursor = max(0, state.cursor-1)
	case "down", "j":
		state.cursor = min(len(agentProviderChoices), state.cursor+1)
	case "enter":
		return configKeyAction{kind: configKeyOpenProvider}
	}
	return configKeyAction{}
}

func (state *configSettingsState) selectProvider(choice agentProviderChoice) bool {
	state.providerCursor = agentProviderIndex(choice.Runtime)
	state.providerRuntime = choice.Runtime
	state.profileCursor = 0
	state.profileEditing = true
	state.status = ""
	state.connectionTitle = ""
	state.connectionError = ""
	index := profileIndexForRuntime(state.working.Profiles, choice.Runtime, state.working.Fix.Profile)
	if index < 0 {
		return false
	}
	profile := state.working.Profiles[index]
	if profile.ID != state.working.Fix.Profile {
		state.pendingActive = profile.ID
	}
	return true
}

func (state *configSettingsState) adjustProfileChoice(catalog agent.ProfileCatalog, direction int) bool {
	index := state.selectedProfileIndex()
	if !state.profileEditing || index < 0 {
		return false
	}
	profile := &state.working.Profiles[index]
	fields := state.profileEditorFields(catalog, *profile)
	fieldIndex := state.profileCursor
	if fieldIndex < 0 || fieldIndex >= len(fields) || fields[fieldIndex].Kind != agent.ProfileFieldChoice {
		return false
	}
	field := fields[fieldIndex]
	state.beginPendingAgentEdit(*profile)
	value := cycleString(profileFieldValue(*profile, field), field.Choices, direction)
	setProfileFieldValue(profile, field, value)
	delete(state.probes, profile.ID)
	delete(state.probing, profile.ID)
	state.pendingActive = profile.ID
	state.dirty = true
	state.status = "Modified"
	return true
}
