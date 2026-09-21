package follow

import (
	tea "github.com/charmbracelet/bubbletea"
)

func dispatchKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	model.reconcileLegacyOverlayStack()
	defer model.reconcileLegacyOverlayStack()
	if model.width > 0 && model.height > 0 && responsiveTier(model.width, model.height) == ResponsiveResize {
		return dispatchResizeKey(model, name, key)
	}
	if overlay, ok := model.overlays.Top(); ok {
		return dispatchOverlayKey(model, overlay.Kind, key)
	}
	if model.mainView == MainViewAgents && model.agents.FindEditing {
		return model, model.agents.handleFindKey(key, makeAgentLayout(model.width, model.height, bodyHeight(model.mainView, model.height)))
	}
	if name == "ctrl+c" || name == "q" {
		return model.requestQuit()
	}
	if handled, command := dispatchGlobalKey(model, name); handled {
		return model, command
	}
	if model.mainView == MainViewAgents {
		return dispatchAgentKey(model, name)
	}
	return dispatchFileKey(model, name)
}

func dispatchResizeKey(model *Model, name string, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if overlay, ok := model.overlays.Top(); ok && overlay.Kind == OverlayShutdown {
		if model.width >= 24 && model.height >= 2 {
			return dispatchOverlayKey(model, overlay.Kind, key)
		}
		return model, nil
	}
	if name == "q" || name == "ctrl+c" {
		return model.requestQuit()
	}
	return model, nil
}

func dispatchGlobalKey(model *Model, name string) (bool, tea.Cmd) {
	switch name {
	case "tab":
		model.toggleMainView()
	case "A":
		model.switchMainView(MainViewAgents)
	case "s":
		model.settings, model.settingsCursor = true, 0
		model.settingsGroup = ""
		model.runtime.filesSettings = false
	case "h":
		model.help, model.helpCursor, model.helpTopic = true, 0, ""
	default:
		return false, nil
	}
	return true, nil
}

func dispatchOverlayKey(model *Model, kind OverlayKind, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	switch kind {
	case OverlayFind:
		return handleFindKey(model, key)
	case OverlayInfo:
		if handleDialogKey(model, name) {
			return model, nil
		}
		return handleInfoKey(model, name)
	case OverlayHelp:
		return handleHelpKey(model, name)
	case OverlayDetail:
		return handleDetailKey(model, name)
	case OverlaySource:
		if name == "f" || name == "/" {
			return openFind(model, true)
		}
		return handleSourceKey(model, key)
	case OverlayColumns:
		return handleColumnKey(model, name)
	case OverlaySort:
		return handleSortKey(model, name)
	case OverlayWeights:
		return handleWeightsKey(model, name)
	case OverlayAppearance:
		return handleAppearanceKey(model, name)
	case OverlaySettings:
		if model.runtime.filesEditing {
			return model.handleFilesExclusionsKey(key)
		}
		return handleSettingsKey(model, name)
	case OverlayConfigSettings:
		return model.handleConfigSettingsKey(key)
	case OverlayFixForm:
		return model.handleFixFormKey(key)
	case OverlayTargetScoreEditor:
		return model.handleFixTargetScoreKey(key)
	case OverlayPromptEditor:
		return model.handleMasterPromptKey(key)
	case OverlayJobMonitor:
		return model.handleJobMonitorKey(key)
	case OverlayJobLog, OverlayJobDiff, OverlayCandidateSource:
		return model.handleJobReaderKey(key)
	case OverlayConfirmation:
		return model.handleCancelConfirmationKey(key)
	case OverlaySettingsDirty:
		return model.handleSettingsDirtyKey(key)
	case OverlayShutdown:
		return model.handleShutdownKey(key)
	case OverlayRuntimeError:
		return handleRuntimeErrorKey(model, name)
	default:
		return model, nil
	}
}

func openSortDialog(model *Model) {
	model.sortOpen = true
	model.files.prepareSortDirections()
	model.files.SortCursor = 0
	for index, item := range sortFields() {
		if item.key == model.files.SortKey && model.files.sortOptionEnabled(index) {
			model.files.SortCursor = index
			break
		}
	}
	if !model.files.sortOptionEnabled(model.files.SortCursor) {
		model.files.moveSortCursor(1)
	}
}
