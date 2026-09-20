package follow

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

type FilesState struct {
	Document             report.Document
	BaseDocument         report.Document
	ScoreDistribution    scoreDistribution
	DisplayFilesCache    []report.File
	DisplayFilesReady    bool
	LongestDisplayPath   int
	FreshnessStatusText  string
	FreshnessStatusReady bool
	Rows                 map[string]rowState
	Selected             string
	Cursor               int
	Offset               int
	HorizontalOffset     int
	SortKey              string
	SortReverse          bool
	SortDirections       map[string]bool
	SortCursor           int
	Marking              bool
	ShiftMarking         bool
	Marked               map[string]bool
	Visible              map[string]bool
}

func (state *FilesState) refreshFreshnessStatus() {
	state.FreshnessStatusText = freshnessStatusForFiles(state.Document.Files)
	state.FreshnessStatusReady = true
}

// refreshDisplayFiles rebuilds the ordered projection used by the table.
// The report remains the source of truth; this cache only stores the current
// view order and its path width for navigation and rendering.
func (state *FilesState) refreshDisplayFiles(limit int) {
	files := append([]report.File(nil), state.Document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return filesLess(state.SortKey, state.SortReverse, files[left], files[right])
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	longest := 0
	for _, file := range files {
		longest = max(longest, lipgloss.Width(file.Path))
	}
	state.DisplayFilesCache = files
	state.DisplayFilesReady = true
	state.LongestDisplayPath = longest
}

func (state FilesState) displayFiles(limit int) []report.File {
	if state.DisplayFilesReady {
		return state.DisplayFilesCache
	}
	files := append([]report.File(nil), state.Document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return filesLess(state.SortKey, state.SortReverse, files[left], files[right])
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files
}

func (state FilesState) sortOptionEnabled(index int) bool {
	field := sortFields()[index]
	return field.key == "score" || field.key == "filename" || state.Visible[field.key]
}

func (state *FilesState) moveSortCursor(delta int) {
	items := sortFields()
	if len(items) == 0 {
		return
	}
	for step := 0; step < len(items); step++ {
		state.SortCursor = (state.SortCursor + delta + len(items)) % len(items)
		if state.sortOptionEnabled(state.SortCursor) {
			return
		}
	}
}

func filesLess(sortKey string, reverse bool, left, right report.File) bool {
	if sortKey != "filename" && (len(left.PendingComponents) > 0) != (len(right.PendingComponents) > 0) {
		return len(left.PendingComponents) == 0
	}
	if sortKey == "filename" {
		comparison := strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path))
		if comparison == 0 {
			comparison = strings.Compare(left.Path, right.Path)
		}
		if reverse {
			return comparison > 0
		}
		return comparison < 0
	}
	leftValue, leftExists := filesSortValue(sortKey, left)
	rightValue, rightExists := filesSortValue(sortKey, right)
	if leftExists != rightExists {
		return leftExists
	}
	if leftValue == rightValue {
		return left.Path < right.Path
	}
	if reverse {
		return leftValue > rightValue
	}
	return leftValue < rightValue
}

func filesSortValue(sortKey string, file report.File) (float64, bool) {
	switch sortKey {
	case "score":
		return file.Score, !metricFailed(file, "score")
	case "cog", "npath", "cyclo", "deep", "god", "coupling", "nesting", "typesafety":
		value, exists, _ := metric(file, sortKey)
		return value, exists
	default:
		return float64(file.Rank), true
	}
}

func (state *FilesState) prepareSortDirections() {
	if state.SortDirections == nil {
		state.SortDirections = make(map[string]bool, len(sortFields()))
		for _, item := range sortFields() {
			state.SortDirections[item.key] = true
		}
	}
	state.SortDirections[state.SortKey] = state.SortReverse
}

func (state FilesState) sortDirection(key string) bool {
	if direction, ok := state.SortDirections[key]; ok {
		return direction
	}
	if key == state.SortKey {
		return state.SortReverse
	}
	return true
}

func (state *FilesState) activateSort(direction, changeDirection bool) bool {
	if !state.sortOptionEnabled(state.SortCursor) {
		return false
	}
	state.prepareSortDirections()
	key := sortFields()[state.SortCursor].key
	if changeDirection {
		state.SortDirections[key] = direction
	}
	state.SortKey = key
	state.SortReverse = state.SortDirections[key]
	return true
}

func (state FilesState) selectedFile(limit int) (report.File, bool) {
	files := state.displayFiles(limit)
	if state.Cursor < 0 || state.Cursor >= len(files) {
		return report.File{}, false
	}
	return files[state.Cursor], true
}

func (state *FilesState) selectCursor(limit int) {
	files := state.displayFiles(limit)
	if state.Cursor >= 0 && state.Cursor < len(files) {
		state.Selected = files[state.Cursor].Path
	}
}

func (state *FilesState) move(delta, limit int) {
	files := state.displayFiles(limit)
	if len(files) == 0 {
		return
	}
	state.Cursor = min(len(files)-1, max(0, state.Cursor+delta))
	state.selectCursor(limit)
}

func (state *FilesState) restoreSelection(limit, page int) {
	files := state.displayFiles(limit)
	for index, file := range files {
		if file.Path == state.Selected {
			state.Cursor = index
			state.ensureVisible(page, len(files))
			return
		}
	}
	if state.Cursor >= len(files) {
		state.Cursor = max(0, len(files)-1)
	}
	if len(files) > 0 {
		state.Selected = files[state.Cursor].Path
	} else {
		state.Selected = ""
	}
	state.ensureVisible(page, len(files))
}

func (state *FilesState) ensureVisible(page, fileCount int) {
	if page <= 0 {
		return
	}
	if state.Cursor < state.Offset {
		state.Offset = state.Cursor
	}
	if state.Cursor >= state.Offset+page {
		state.Offset = state.Cursor - page + 1
	}
	maxOffset := max(0, fileCount-page)
	state.Offset = min(state.Offset, maxOffset)
}

func (state FilesState) maxPathOffset(viewportWidth, limit int) int {
	if viewportWidth <= 0 {
		return 0
	}
	if state.DisplayFilesReady {
		return max(0, state.LongestDisplayPath-viewportWidth)
	}
	longest := 0
	for _, file := range state.displayFiles(limit) {
		longest = max(longest, lipgloss.Width(file.Path))
	}
	return max(0, longest-viewportWidth)
}

func (state *FilesState) toggleMark(path string) {
	if state.Marked == nil {
		state.Marked = map[string]bool{}
	}
	if state.Marked[path] {
		delete(state.Marked, path)
		return
	}
	state.Marked[path] = true
}

func (state *FilesState) toggleMarkMode() {
	state.Marking = !state.Marking
	state.ShiftMarking = false
}

func (state *FilesState) toggleCurrentMark(limit int) bool {
	files := state.displayFiles(limit)
	if state.Cursor < 0 || state.Cursor >= len(files) {
		return false
	}
	state.toggleMark(files[state.Cursor].Path)
	return true
}

func (state *FilesState) moveAndToggleMark(delta, limit, page int) bool {
	files := state.displayFiles(limit)
	before := state.Cursor
	startingRange := !state.ShiftMarking
	state.move(delta, limit)
	state.ensureVisible(page, len(files))
	if state.Cursor == before || before < 0 || before >= len(files) {
		return false
	}
	if startingRange {
		state.toggleMark(files[before].Path)
	}
	state.toggleCurrentMark(limit)
	state.ShiftMarking = true
	return true
}

func (state *FilesState) clearMarks() {
	state.Marked = map[string]bool{}
	state.ShiftMarking = false
}

func (state FilesState) markedCount() int { return len(state.Marked) }

func (state FilesState) markColumnWidth() int {
	if state.Marking {
		return 4
	}
	if state.markedCount() > 0 {
		return 2
	}
	return 0
}

func (state FilesState) fileMarkPrefix(path string, background lipgloss.Color) string {
	marked := state.Marked[path]
	if state.Marking {
		mark := " "
		if marked {
			mark = "●"
		}
		return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render("(" + mark + ") ")
	}
	if state.markedCount() == 0 {
		return ""
	}
	mark := "  "
	if marked {
		mark = "● "
	}
	return lipgloss.NewStyle().Background(background).Foreground(style.TextPrimary).Bold(marked).Render(mark)
}

func (state *FilesState) pruneMarks() {
	if len(state.Marked) == 0 {
		return
	}
	available := make(map[string]bool, len(state.Document.Files))
	for _, file := range state.Document.Files {
		available[file.Path] = true
	}
	for path := range state.Marked {
		if !available[path] {
			delete(state.Marked, path)
		}
	}
}

func (state FilesState) markedPaths(limit int) []string {
	if len(state.Marked) == 0 {
		return nil
	}
	result := make([]string, 0, len(state.Marked))
	seen := make(map[string]bool, len(state.Marked))
	for _, file := range state.displayFiles(limit) {
		if state.Marked[file.Path] {
			result = append(result, file.Path)
			seen[file.Path] = true
		}
	}
	for _, file := range state.Document.Files {
		if state.Marked[file.Path] && !seen[file.Path] {
			result = append(result, file.Path)
		}
	}
	return result
}
