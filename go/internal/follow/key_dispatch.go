package follow

import tea "github.com/charmbracelet/bubbletea"

func dispatchKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	model.reconcileLegacyOverlayStack()
	defer model.reconcileLegacyOverlayStack()
	if overlay, ok := model.overlays.Top(); ok {
		return dispatchOverlayKey(model, overlay.Kind, key)
	}
	if model.mainView == MainViewAgents && model.agents.FindEditing {
		return model.handleAgentFindKey(key)
	}
	switch name {
	case "tab":
		model.toggleMainView()
		return model, nil
	case "A":
		model.switchMainView(MainViewAgents)
		return model, nil
	case "ctrl+c", "q":
		return model.requestQuit()
	case "s":
		model.settings = true
		model.settingsCursor = 0
		return model, nil
	case "h":
		model.help = true
		model.helpCursor = 0
		model.helpTopic = ""
		return model, nil
	}
	if model.mainView == MainViewAgents {
		switch name {
		case "up", "k":
			model.moveAgentSelection(-1)
		case "down", "j":
			model.moveAgentSelection(1)
		case "left":
			model.moveAgentHorizontal(-pathScrollStep)
		case "right":
			model.moveAgentHorizontal(pathScrollStep)
		case "ctrl+f", "pgdown":
			model.pageAgentSelection(1)
		case "ctrl+b", "pgup":
			model.pageAgentSelection(-1)
		case "home", "g":
			model.jumpAgentSelection(false)
		case "end", "G":
			model.jumpAgentSelection(true)
		case "enter":
			if model.agents.Selected.IsJob() {
				model.toggleSelectedAgentJob()
			} else {
				return model, model.openJobMonitor(model.agents.Selected.JobID, model.agents.Selected.Path)
			}
		case "a":
			model.toggleAgentFilter()
		case "f", "/":
			model.beginAgentFind()
		case "o":
			model.cycleAgentSort(1)
		case "O":
			model.agents.SortReverse = !model.agents.SortReverse
			model.reconcileAgentSelection()
		case "i":
			return model, model.openJobMonitor(model.agents.Selected.JobID, model.agents.Selected.Path)
		case "d":
			return model, model.openJobDiff(model.agents.Selected.JobID, model.agents.Selected.Path)
		case "l":
			if model.agents.Selected.IsJob() {
				return model, model.openJobLog(model.agents.Selected.JobID)
			}
		case "v":
			if !model.agents.Selected.IsJob() {
				return model, model.openCandidateSource(model.agents.Selected.JobID, model.agents.Selected.Path)
			}
		case "C":
			model.openCancelConfirmation()
		case " ":
			model.openJobActions()
		}
		return model, nil
	}
	switch name {
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
	case "home", "g":
		model.cursor = 0
		model.selectCursor()
		model.ensureVisible()
	case "end", "G":
		model.cursor = max(0, len(model.displayFiles())-1)
		model.selectCursor()
		model.ensureVisible()
	case "enter", "i":
		model.openSelectedFileInfo()
	case "x":
		return model, model.openFixForSelected()
	case "v":
		return model, model.openSourceView()
	case "c":
		model.settings = true
		model.settingsCursor = settingsIndex("columns")
	case "o":
		openSortDialog(model)
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

func dispatchOverlayKey(model *Model, kind OverlayKind, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	switch kind {
	case OverlayFind:
		return model.handleFindKey(key)
	case OverlayInfo:
		if model.handleDialogKey(name) {
			return model, nil
		}
		return model.handleInfoKey(name)
	case OverlayHelp:
		return model.handleHelpKey(name)
	case OverlayDetail:
		return model.handleDetailKey(name)
	case OverlaySource:
		if name == "f" || name == "/" {
			return model.openFind(true)
		}
		return model.handleSourceKey(key)
	case OverlayColumns:
		return model.handleColumnKey(name)
	case OverlaySort:
		return model.handleSortKey(name)
	case OverlayWeights:
		return model.handleWeightsKey(name)
	case OverlayAppearance:
		return model.handleAppearanceKey(name)
	case OverlaySettings:
		return model.handleSettingsKey(name)
	case OverlayConfigSettings:
		return model.handleConfigSettingsKey(key)
	case OverlayFixForm:
		return model.handleFixFormKey(key)
	case OverlayPromptEditor:
		return model.handlePromptEditorKey(key)
	case OverlayPromptDetach:
		return model.handlePromptDetachKey(key)
	case OverlayPromptDirty:
		return model.handlePromptDirtyKey(key)
	case OverlayJobMonitor:
		return model.handleJobMonitorKey(key)
	case OverlayJobActions:
		return model.handleJobActionsKey(key)
	case OverlayJobLog, OverlayJobDiff, OverlayCandidateSource:
		return model.handleJobReaderKey(key)
	case OverlayConfirmation:
		return model.handleCancelConfirmationKey(key)
	case OverlaySettingsDirty:
		return model.handleSettingsDirtyKey(key)
	case OverlayShutdown:
		return model.handleShutdownKey(key)
	default:
		return model, nil
	}
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
