package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
)

func (state *configSettingsState) handleBrowseKey(key tea.KeyMsg, catalog agent.ProfileCatalog, rows int) configKeyAction {
	name := key.String()
	if state.kind == configFix && state.cursor >= fixSettingsMetricStart && isToggleKey(name) {
		return configKeyAction{kind: configKeyAdjust, direction: 1}
	}
	switch name {
	case "esc", "escape", "q":
		return configKeyAction{kind: configKeyClose}
	case "up", "k":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = max(fixSettingsPromptRow, state.cursor-3)
		} else {
			state.cursor = max(0, state.cursor-1)
		}
	case "down", "j":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = min(max(0, rows-1), state.cursor+3)
		} else {
			state.cursor = min(max(0, rows-1), state.cursor+1)
		}
	case "left", "h", "-":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = max(fixSettingsMetricStart, state.cursor-1)
		} else if len(state.choices(state.cursor)) > 1 {
			return configKeyAction{}
		} else {
			return configKeyAction{kind: configKeyAdjust, direction: -1}
		}
	case "right", "l", "+", "=":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = min(max(0, rows-1), state.cursor+1)
		} else if len(state.choices(state.cursor)) > 1 {
			return configKeyAction{}
		} else {
			return configKeyAction{kind: configKeyAdjust, direction: 1}
		}
	case " ":
		return configKeyAction{kind: configKeyAdjust, direction: 1}
	case "enter":
		if len(state.choices(state.cursor)) > 1 {
			return configKeyAction{kind: configKeyOpenChoice}
		}
		if state.kind == configFix && state.cursor == fixSettingsPromptRow {
			state.prompt = newMasterPromptTextBox(state.working.Fix.PromptTemplate)
			state.promptOriginal = state.working.Fix.PromptTemplate
			state.promptError = ""
			state.prompt.Focus()
			return configKeyAction{kind: configKeyOpenPrompt}
		}
		if state.textEditable() {
			state.beginText(catalog)
			return configKeyAction{}
		}
		return configKeyAction{kind: configKeyAdjust, direction: 1}
	}
	return configKeyAction{}
}
