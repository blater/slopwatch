package follow

import tea "github.com/charmbracelet/bubbletea"

func (model Model) Init() tea.Cmd { return initModel(model) }

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	return updateModel(model, message)
}

func (model Model) View() string { return view(model) }
