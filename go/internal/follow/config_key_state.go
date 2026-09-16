package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
)

type configKeyActionKind uint8

const (
	configKeyNone configKeyActionKind = iota
	configKeyClose
	configKeySave
	configKeyTestProfile
	configKeyOpenChoice
	configKeyOpenPrompt
	configKeyOpenProvider
	configKeyAdjust
	configKeyHandleChoice
)

type configKeyAction struct {
	kind      configKeyActionKind
	direction int
	command   tea.Cmd
}

func (state *configSettingsState) handleKey(key tea.KeyMsg, catalog agent.ProfileCatalog, rows int) configKeyAction {
	if state.editing {
		return state.handleEditingKey(key, catalog)
	}
	if state.loading || state.saving {
		if state.loading && (key.String() == "esc" || key.String() == "escape") {
			return configKeyAction{kind: configKeyClose}
		}
		return configKeyAction{}
	}
	if state.kind == configAgents {
		return state.handleAgentKey(key, catalog)
	}
	if state.choiceOpen {
		return configKeyAction{kind: configKeyHandleChoice}
	}
	return state.handleBrowseKey(key, catalog, rows)
}
