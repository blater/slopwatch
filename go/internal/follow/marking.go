package follow

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopmochi/internal/style"
)

func (model *Model) toggleMarkMode() {
	model.marking = !model.marking
	model.shiftMarking = false
	model.pathOffset = min(model.pathOffset, model.maxPathOffset())
}

func (model *Model) toggleCurrentMark() {
	files := model.displayFiles()
	if model.cursor < 0 || model.cursor >= len(files) {
		return
	}
	model.toggleMarkedPath(files[model.cursor].Path)
}

func (model *Model) toggleMarkedPath(path string) {
	if model.marked == nil {
		model.marked = map[string]bool{}
	}
	if model.marked[path] {
		delete(model.marked, path)
		return
	}
	model.marked[path] = true
}

func (model *Model) moveAndToggleMark(delta int) {
	files := model.displayFiles()
	before := model.cursor
	startingRange := !model.shiftMarking
	model.move(delta)
	if model.cursor == before || before < 0 || before >= len(files) {
		return
	}
	if startingRange {
		model.toggleMarkedPath(files[before].Path)
	}
	model.toggleCurrentMark()
	model.shiftMarking = true
}

func (model *Model) clearMarkedFiles() {
	model.marked = map[string]bool{}
	model.shiftMarking = false
	model.pathOffset = min(model.pathOffset, model.maxPathOffset())
}

func (model Model) markedCount() int {
	return len(model.marked)
}

func (model Model) markColumnWidth() int {
	if model.marking {
		return 4
	}
	if model.markedCount() > 0 {
		return 2
	}
	return 0
}

func (model Model) fileMarkPrefix(path string, background lipgloss.Color) string {
	marked := model.marked[path]
	if model.marking {
		mark := " "
		if marked {
			mark = "●"
		}
		return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render("(" + mark + ") ")
	}
	if model.markedCount() == 0 {
		return ""
	}
	mark := "  "
	if marked {
		mark = "● "
	}
	return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render(mark)
}

func (model *Model) pruneMarkedFiles() {
	if len(model.marked) == 0 {
		return
	}
	available := make(map[string]bool, len(model.document.Files))
	for _, file := range model.document.Files {
		available[file.Path] = true
	}
	for path := range model.marked {
		if !available[path] {
			delete(model.marked, path)
		}
	}
	model.pathOffset = min(model.pathOffset, model.maxPathOffset())
}

func (model Model) markedFilePaths() []string {
	if len(model.marked) == 0 {
		return nil
	}
	result := make([]string, 0, len(model.marked))
	seen := make(map[string]bool, len(model.marked))
	for _, file := range model.displayFiles() {
		if model.marked[file.Path] {
			result = append(result, file.Path)
			seen[file.Path] = true
		}
	}
	for _, file := range model.document.Files {
		if model.marked[file.Path] && !seen[file.Path] {
			result = append(result, file.Path)
		}
	}
	return result
}

func markedFilesLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return formatIntegerWithCommas(count) + " files"
}
