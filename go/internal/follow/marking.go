package follow

func (model *Model) toggleMarkMode() {
	model.files.toggleMarkMode()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
}

func (model *Model) toggleCurrentMark() {
	model.files.toggleCurrentMark(model.options.Limit)
}

func (model *Model) moveAndToggleMark(delta int) {
	model.files.moveAndToggleMark(delta, model.options.Limit, model.bodyHeight())
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
