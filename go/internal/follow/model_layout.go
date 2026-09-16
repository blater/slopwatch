package follow

func (model Model) bodyHeight() int {
	reserved := 3
	if model.mainView == MainViewFiles {
		reserved = 4
	}
	return max(1, model.height-reserved)
}

func (model *Model) ensureVisible() {
	model.files.ensureVisible(model.bodyHeight(), len(model.files.displayFiles(model.options.Limit)))
}

func (model Model) mainViewContent() string {
	if model.mainView == MainViewAgents {
		return agentsTableView(model)
	}
	return tableView(model)
}
