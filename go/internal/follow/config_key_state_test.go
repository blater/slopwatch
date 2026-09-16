package follow

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfigKeyStateKeepsEditingAndLoadingGuardsBeforeChoice(t *testing.T) {
	state := configSettingsState{kind: configFix, choiceOpen: true, saving: true, cursor: 2}
	if action := state.handleKey(tea.KeyMsg{Type: tea.KeyDown}, nil, 8); action.kind != configKeyNone || state.cursor != 2 {
		t.Fatalf("saving state handled choice key: action=%+v cursor=%d", action, state.cursor)
	}

	state.saving = false
	state.loading = true
	if action := state.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, nil, 8); action.kind != configKeyClose {
		t.Fatalf("loading escape action=%v, want close", action.kind)
	}

	state.loading = false
	state.editing = true
	state.input = textinput.New()
	if action := state.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, nil, 8); action.kind != configKeyNone || state.editing {
		t.Fatalf("editing escape did not win over choice: action=%v editing=%t", action.kind, state.editing)
	}
}
