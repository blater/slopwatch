package follow

import tea "github.com/charmbracelet/bubbletea"

func dispatchKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if model.findOpen {
		return model.handleFindKey(key)
	}
	if model.infoOpen {
		if model.handleDialogKey(name) {
			return model, nil
		}
		return model.handleInfoKey(name)
	}
	if model.help {
		return model.handleHelpKey(name)
	}
	if model.detail {
		return model.handleDetailKey(name)
	}
	if model.sourceView {
		if name == "f" || name == "/" {
			return model.openFind(true)
		}
		return model.handleSourceKey(key)
	}
	if model.columns {
		return model.handleColumnKey(name)
	}
	if model.sortOpen {
		return model.handleSortKey(name)
	}
	if model.weightsOpen {
		return model.handleWeightsKey(name)
	}
	if model.settings {
		return model.handleSettingsKey(name)
	}
	switch name {
	case "ctrl+c", "q":
		return model, tea.Quit
	case "up", "k":
		model.move(-1)
	case "down", "j":
		model.move(1)
	case "left":
		model.movePath(-pathScrollStep)
	case "right":
		model.movePath(pathScrollStep)
	case "ctrl+f", "pgdown":
		model.move(max(1, model.bodyHeight()))
	case "ctrl+b", "pgup":
		model.move(-max(1, model.bodyHeight()))
	case "home":
		model.cursor = 0
		model.selectCursor()
		model.ensureVisible()
	case "end":
		model.cursor = max(0, len(model.displayFiles())-1)
		model.selectCursor()
		model.ensureVisible()
	case "enter", "i":
		model.openSelectedFileInfo()
	case "v":
		return model, model.openSourceView()
	case "c":
		model.settings = true
		model.settingsCursor = 1
	case "s":
		model.settings = true
		model.settingsCursor = 0
	case "o":
		openSortDialog(model)
	case "h":
		model.help = true
		model.helpCursor = 0
	case "f", "/":
		return model.openFind(false)
	case "n":
		if model.findQuery != "" {
			model.findNext(1)
		}
	case "N":
		if model.findQuery != "" {
			model.findNext(-1)
		}
	case "r":
		if !model.analyzing {
			model.analyzing = true
			return model, model.analyze(nil, true)
		}
	}
	return model, nil
}

func openSortDialog(model *Model) {
	model.sortOpen = true
	model.prepareSortDirections()
	model.sortCursor = 0
	for index, item := range sortFields() {
		if item.key == model.sortKey && model.sortOptionEnabled(index) {
			model.sortCursor = index
			break
		}
	}
	if !model.sortOptionEnabled(model.sortCursor) {
		model.moveSortCursor(1)
	}
}
