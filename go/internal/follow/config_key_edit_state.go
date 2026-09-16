package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
)

func (state *configSettingsState) handleEditingKey(key tea.KeyMsg, catalog agent.ProfileCatalog) configKeyAction {
	switch key.String() {
	case "esc", "escape":
		state.editing = false
		state.input.Blur()
		return configKeyAction{}
	case "enter":
		if err := state.commitText(catalog); err != nil {
			state.status = err.Error()
			return configKeyAction{}
		}
		state.editing = false
		state.input.Blur()
		if state.kind == configAgents {
			return configKeyAction{kind: configKeyTestProfile}
		}
		return configKeyAction{kind: configKeySave}
	default:
		var command tea.Cmd
		state.input, command = state.input.Update(key)
		return configKeyAction{command: command}
	}
}
