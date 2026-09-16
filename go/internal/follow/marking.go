package follow

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/style"
)

func (model *Model) toggleMarkMode() {
	model.files.Marking = !model.files.Marking
	model.files.ShiftMarking = false
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
}

func (model *Model) toggleCurrentMark() {
	files := model.files.displayFiles(model.options.Limit)
	if model.files.Cursor < 0 || model.files.Cursor >= len(files) {
		return
	}
	model.files.toggleMark(files[model.files.Cursor].Path)
}

func (model *Model) moveAndToggleMark(delta int) {
	files := model.files.displayFiles(model.options.Limit)
	before := model.files.Cursor
	startingRange := !model.files.ShiftMarking
	move(model, delta)
	if model.files.Cursor == before || before < 0 || before >= len(files) {
		return
	}
	if startingRange {
		model.files.toggleMark(files[before].Path)
	}
	model.toggleCurrentMark()
	model.files.ShiftMarking = true
}

func (model *Model) clearMarkedFiles() {
	model.files.clearMarks()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
}

func (model Model) markColumnWidth() int {
	if model.files.Marking {
		return 4
	}
	if model.files.markedCount() > 0 {
		return 2
	}
	return 0
}

func (model Model) fileMarkPrefix(path string, background lipgloss.Color) string {
	marked := model.files.Marked[path]
	if model.files.Marking {
		mark := " "
		if marked {
			mark = "●"
		}
		return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render("(" + mark + ") ")
	}
	if model.files.markedCount() == 0 {
		return ""
	}
	mark := "  "
	if marked {
		mark = "● "
	}
	return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render(mark)
}

func markedFilesLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return formatIntegerWithCommas(count) + " files"
}
