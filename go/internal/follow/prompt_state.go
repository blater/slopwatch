package follow

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type promptKeyResult struct {
	close   bool
	save    bool
	command tea.Cmd
}

func (state *configSettingsState) handlePromptKey(key tea.KeyMsg, width, height int) promptKeyResult {
	switch key.String() {
	case "ctrl+s":
		value := strings.TrimSpace(cleanEditorText(state.prompt.Value()))
		if value == "" {
			state.promptError = "Agent prompt cannot be empty"
			resizeMasterPromptTextBox(state, width, height)
			return promptKeyResult{}
		}
		state.promptError = ""
		state.working.Fix.PromptTemplate = value
		state.promptOriginal = value
		state.prompt.Blur()
		state.dirty = true
		return promptKeyResult{close: true, save: true}
	case "esc", "escape":
		state.prompt.SetValue(state.promptOriginal)
		state.promptError = ""
		state.prompt.Blur()
		return promptKeyResult{close: true}
	default:
		updated, command := state.prompt.Update(key)
		state.prompt = updated
		state.promptError = ""
		resizeMasterPromptTextBox(state, width, height)
		return promptKeyResult{command: command}
	}
}
