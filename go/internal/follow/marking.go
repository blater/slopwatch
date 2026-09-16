package follow

func (model *Model) toggleMarkMode() {
	model.files.toggleMarkMode()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
}

func (model *Model) moveAndToggleMark(delta int) {
	model.files.moveAndToggleMark(delta, model.options.Limit, bodyHeight(model.mainView, model.height))
}

func (model *Model) clearMarkedFiles() {
	model.files.clearMarks()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
}

func markedFilesLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return formatIntegerWithCommas(count) + " files"
}
