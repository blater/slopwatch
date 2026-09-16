package follow

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

type shutdownKeyOutcome struct {
	close   bool
	command tea.Cmd
}

func (state *shutdownState) handleKey(key tea.KeyMsg, service FixService) shutdownKeyOutcome {
	if state.pending {
		return shutdownKeyOutcome{}
	}
	switch key.String() {
	case "esc", "q":
		return shutdownKeyOutcome{close: true}
	case "enter", "y":
		state.pending = true
		return shutdownKeyOutcome{command: func() tea.Msg {
			return shutdownCompleteMsg{err: service.Shutdown(context.Background())}
		}}
	}
	return shutdownKeyOutcome{}
}

func (state *shutdownState) complete(message shutdownCompleteMsg) tea.Cmd {
	state.pending = false
	if message.err != nil {
		state.errorText = message.err.Error()
		return nil
	}
	return tea.Quit
}
