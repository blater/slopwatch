package follow

func bodyHeight(mainView MainView, height int) int {
	reserved := 3
	if mainView == MainViewFiles {
		reserved = 4
	}
	return max(1, height-reserved)
}

func (model *Model) ensureVisible() {
	model.files.ensureVisible(bodyHeight(model.mainView, model.height), len(model.files.displayFiles(model.options.Limit)))
}

func (model Model) mainViewContent() string {
	if model.mainView == MainViewAgents {
		return agentsTableView(model)
	}
	return tableView(model)
}
