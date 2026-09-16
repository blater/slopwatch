package follow

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/style"
)

var appearanceThemes = []struct {
	label string
	theme style.Theme
}{
	{label: "Dark", theme: style.ThemeDark},
	{label: "Light", theme: style.ThemeLight},
}

func handleAppearanceKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if isToggleKey(name) {
		model.selectAppearance()
		model.appearance = false
		model.settings = true
		return model, nil
	}
	switch name {
	case "esc", "escape", "q":
		model.appearance = false
		model.settings = true
	case "up", "k":
		model.appearanceCursor = max(0, model.appearanceCursor-1)
	case "down", "j":
		model.appearanceCursor = min(len(appearanceThemes)-1, model.appearanceCursor+1)
	}
	return model, nil
}

func (model *Model) selectAppearance() {
	model.theme = appearanceThemes[model.appearanceCursor].theme
	ConfigureTheme(model.theme)
	model.persistUserPreferences()
}

func appearanceView(model Model) string {
	content := make([]string, 0, len(appearanceThemes))
	for index, item := range appearanceThemes {
		mark := " "
		if item.theme == model.theme || (model.theme == "" && item.theme == style.ThemeDark) {
			mark = "✓"
		}
		content = append(content, style.ToggleOption(fmt.Sprintf("[%s]", mark), item.label, index == model.appearanceCursor, false, 34))
	}
	return style.Popup(style.Heading("APPEARANCE"), content, "", 38)
}
