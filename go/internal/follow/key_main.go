package follow

import (
	tea "github.com/charmbracelet/bubbletea"
)

func dispatchAgentKey(model *Model, name string) (tea.Model, tea.Cmd) {
	switch name {
	case "up", "k":
		model.agents.moveSelection(-1, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "down", "j":
		model.agents.moveSelection(1, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "left":
		policy := model.agentMetricPolicy()
		model.agents.moveHorizontal(pathScrollStep, maximumAgentHorizontalOffset(model.agents.rows(), responsiveTier(model.width, model.height), model.width, policy.visible))
	case "right":
		policy := model.agentMetricPolicy()
		model.agents.moveHorizontal(-pathScrollStep, maximumAgentHorizontalOffset(model.agents.rows(), responsiveTier(model.width, model.height), model.width, policy.visible))
	case "ctrl+f", "pgdown":
		model.agents.pageSelection(1, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "ctrl+b", "pgup":
		model.agents.pageSelection(-1, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "home", "g":
		model.agents.jumpSelection(false, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "end", "G":
		model.agents.jumpSelection(true, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "enter":
		if model.agents.Selected.IsJob() {
			model.agents.toggleSelectedJob(makeAgentLayout(model.width, model.height, model.bodyHeight()))
			break
		}
		return model, model.openJobMonitor(model.agents.Selected.JobID, model.agents.Selected.Path)
	case "a":
		model.agents.toggleFilter(makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "f", "/":
		model.agents.beginFind()
	case "o":
		model.agents.cycleSort(1, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "O":
		model.agents.SortReverse = !model.agents.SortReverse
		model.agents.reconcileSelection(makeAgentLayout(model.width, model.height, model.bodyHeight()))
	case "i":
		return model, model.openJobMonitor(model.agents.Selected.JobID, model.agents.Selected.Path)
	case "d":
		return model, model.openJobReader(OverlayJobDiff, model.agents.Selected.JobID, model.agents.Selected.Path)
	case "l":
		if !model.agents.Selected.IsZero() {
			return model, model.openJobReader(OverlayJobLog, model.agents.Selected.JobID, "")
		}
	case "v":
		if !model.agents.Selected.IsJob() {
			return model, model.openJobReader(OverlayCandidateSource, model.agents.Selected.JobID, model.agents.Selected.Path)
		}
	case "C":
		model.openCancelConfirmation()
	}
	return model, nil
}

func dispatchFileKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if name != "shift+up" && name != "shift+down" {
		model.files.ShiftMarking = false
	}
	switch name {
	case "up", "k":
		move(model, -1)
	case "down", "j":
		move(model, 1)
	case "shift+up":
		if model.files.Marking {
			model.moveAndToggleMark(-1)
		} else {
			move(model, -1)
		}
	case "shift+down":
		if model.files.Marking {
			model.moveAndToggleMark(1)
		} else {
			move(model, 1)
		}
	case "left":
		model.movePath(-pathScrollStep)
	case "right":
		model.movePath(pathScrollStep)
	case "ctrl+f", "pgdown":
		move(model, max(1, model.bodyHeight()))
	case "ctrl+b", "pgup":
		move(model, -max(1, model.bodyHeight()))
	case "home", "g":
		model.files.Cursor = 0
		model.files.selectCursor(model.options.Limit)
		model.ensureVisible()
	case "end", "G":
		model.files.Cursor = max(0, len(model.files.displayFiles(model.options.Limit))-1)
		model.files.selectCursor(model.options.Limit)
		model.ensureVisible()
	case "enter", "i":
		openSelectedFileInfo(model)
	case "x":
		return model, model.openFixForSelected()
	case "m":
		model.toggleMarkMode()
	case "M":
		model.clearMarkedFiles()
	case " ":
		if model.files.Marking {
			model.files.toggleCurrentMark(model.options.Limit)
		}
	case "v":
		return model, openSourceView(model)
	case "c":
		model.settings, model.settingsCursor = true, settingsIndex("columns")
	case "o":
		openSortDialog(model)
	case "f", "/":
		return openFind(model, false)
	case "n":
		if model.source.findQuery != "" {
			findNext(model, 1)
		}
	case "N":
		if model.source.findQuery != "" {
			findNext(model, -1)
		}
	}
	return model, nil
}
