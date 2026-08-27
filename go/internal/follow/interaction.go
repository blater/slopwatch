package follow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
)

const pathScrollStep = 4

func updateModel(model *Model, message tea.Msg) (tea.Model, tea.Cmd) {
	model.reconcileLegacyOverlayStack()
	updated, command := handleMessage(model, message)
	if result, ok := updated.(*Model); ok {
		result.reconcileLegacyOverlayStack()
	}
	return updated, command
}

func markFreshness(model *Model, paths []string, freshness report.Freshness, note string) {
	wanted := pathSet(paths)
	changed := false
	for index := range model.baseDocument.Files {
		file := &model.baseDocument.Files[index]
		if len(wanted) == 0 || wanted[file.Path] {
			file.Freshness = freshness
			file.FreshnessNote = note
			changed = true
		}
	}
	if changed {
		model.rebuildWeightedDocument()
	}
}

func pathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[filepath.ToSlash(path)] = true
	}
	return result
}

func analyzeExisting(model Model, paths []string) tea.Cmd {
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(filepath.Join(model.options.Workspace, filepath.FromSlash(path))); err == nil && !info.IsDir() {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return func() tea.Msg { return analysisResult{replace: paths, document: report.Document{}} }
	}
	command := model.analyze(existing, false)
	return func() tea.Msg {
		result := command()
		analysis := result.(analysisResult)
		analysis.replace = paths
		return analysis
	}
}

func takeQueue(model *Model) []string {
	paths := make([]string, 0, len(model.queued))
	for path := range model.queued {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	model.queued = map[string]bool{}
	return paths
}

func mergeDocument(model *Model, result analysisResult) {
	if len(model.baseDocument.Files) == 0 && len(model.document.Files) > 0 {
		model.baseDocument = model.document
	}
	if result.full {
		model.baseDocument = result.document
		return
	}
	replace := map[string]bool{}
	for _, path := range result.replace {
		replace[filepath.ToSlash(path)] = true
	}
	kept := make([]report.File, 0, len(model.baseDocument.Files)+len(result.document.Files))
	for _, file := range model.baseDocument.Files {
		if !replace[file.Path] {
			kept = append(kept, file)
		}
	}
	model.baseDocument.Files = append(kept, result.document.Files...)
}

func compareScore(current, previous float64) int {
	switch {
	case current < previous:
		return -1
	case current > previous:
		return 1
	default:
		return 0
	}
}

func merge(model *Model, result analysisResult) {
	oldScores := map[string]float64{}
	oldRanks := map[string]int{}
	for _, file := range model.document.Files {
		oldScores[file.Path] = file.Score
		oldRanks[file.Path] = file.Rank
	}
	model.mergeDocument(result)
	model.rebuildWeightedDocument()
	model.document.SortAndRank()
	model.pruneMarkedFiles()
	now := time.Now()
	model.mergeRows(result, oldScores, oldRanks, now, result.full && len(oldScores) == 0)
	model.refreshDisplayFiles()
	model.restoreSelection()
}

func contains(paths []string, path string) bool {
	for _, candidate := range paths {
		if filepath.ToSlash(candidate) == path {
			return true
		}
	}
	return false
}

func restoreSelection(model *Model) {
	for index, file := range model.displayFiles() {
		if file.Path == model.selected {
			model.cursor = index
			model.ensureVisible()
			return
		}
	}
	if model.cursor >= len(model.displayFiles()) {
		model.cursor = max(0, len(model.displayFiles())-1)
	}
	if files := model.displayFiles(); len(files) > 0 {
		model.selected = files[model.cursor].Path
	} else {
		model.selected = ""
	}
	model.ensureVisible()
}

func refreshDisplayFiles(model *Model) {
	files := append([]report.File(nil), model.document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return model.less(files[left], files[right])
	})
	if model.options.Limit > 0 && len(files) > model.options.Limit {
		files = files[:model.options.Limit]
	}
	longest := 0
	for _, file := range files {
		longest = max(longest, lipgloss.Width(file.Path))
	}
	model.displayFilesCache = files
	model.displayFilesReady = true
	model.longestDisplayPath = longest
}

func displayFiles(model Model) []report.File {
	if model.displayFilesReady {
		return model.displayFilesCache
	}
	// Keep value-constructed Models useful for tests and external callers. The
	// application eagerly refreshes this cache whenever ordering can change.
	files := append([]report.File(nil), model.document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return model.less(files[left], files[right])
	})
	if model.options.Limit > 0 && len(files) > model.options.Limit {
		files = files[:model.options.Limit]
	}
	return files
}

type sortField struct {
	column
}

func sortFields() []sortField {
	fields := make([]sortField, 0, len(columnDefinitions)+1)
	for _, column := range columnDefinitions {
		if column.key == "fix" {
			continue
		}
		fields = append(fields, sortField{column: column})
	}
	fields = append(fields, sortField{column: column{key: "filename", title: "PATH", shortDescription: "filename", width: 1, defaultVisible: true}})
	return fields
}

func sortOptionEnabled(model Model, index int) bool {
	field := sortFields()[index]
	return field.key == "score" || field.key == "filename" || model.visible[field.key]
}

func moveSortCursor(model *Model, delta int) {
	items := sortFields()
	if len(items) == 0 {
		return
	}
	for step := 0; step < len(items); step++ {
		model.sortCursor = (model.sortCursor + delta + len(items)) % len(items)
		if model.sortOptionEnabled(model.sortCursor) {
			return
		}
	}
}

func less(model Model, left, right report.File) bool {
	if model.sortKey == "filename" {
		comparison := strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path))
		if comparison == 0 {
			comparison = strings.Compare(left.Path, right.Path)
		}
		if model.sortReverse {
			return comparison > 0
		}
		return comparison < 0
	}
	leftValue, leftExists := model.sortValue(left)
	rightValue, rightExists := model.sortValue(right)
	if leftExists != rightExists {
		return leftExists // unavailable measurements always sort last
	}
	if leftValue == rightValue {
		return left.Path < right.Path
	}
	if model.sortReverse {
		return leftValue > rightValue
	}
	return leftValue < rightValue
}

func sortValue(model Model, file report.File) (float64, bool) {
	switch model.sortKey {
	case "score":
		return file.Score, true
	case "cog", "npath", "cyclo", "deep", "god", "coupling", "nesting", "typesafety":
		value, exists, _ := metric(file, model.sortKey)
		return value, exists
	default:
		return float64(file.Rank), true
	}
}

func handleDetailKey(model *Model, name string) (tea.Model, tea.Cmd) {
	switch name {
	case "esc", "escape", "q":
		model.detail = false
		model.detailOffset = 0
	case "up", "k":
		model.detailOffset = max(0, model.detailOffset-1)
	case "down", "j":
		model.detailOffset = min(model.detailMaxOffset(), model.detailOffset+1)
	case "ctrl+f", "pgdown":
		model.detailOffset = min(model.detailMaxOffset(), model.detailOffset+max(1, model.detailBodyHeight()-1))
	case "ctrl+b", "pgup":
		model.detailOffset = max(0, model.detailOffset-max(1, model.detailBodyHeight()-1))
	case "home", "g":
		model.detailOffset = 0
	case "end", "G":
		model.detailOffset = model.detailMaxOffset()
	}
	return model, nil
}

func handleSourceKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if name == "esc" || name == "escape" || name == "q" || name == "v" {
		model.sourceView = false
		model.sourceLoading = false
		model.sourcePath = ""
		model.sourceLastKey = ""
		model.sourceLastAt = time.Time{}
		model.sourceRapid = 0
		return model, nil
	}
	if name == "ctrl+f" {
		model.sourceViewport.PageDown()
		return model, nil
	}
	if name == "ctrl+b" {
		model.sourceViewport.PageUp()
		return model, nil
	}
	if name == "n" && model.findQuery != "" {
		model.findNext(1)
		return model, nil
	}
	if name == "N" && model.findQuery != "" {
		model.findNext(-1)
		return model, nil
	}
	if name == "down" || name == "j" {
		model.sourceViewport.ScrollDown(model.sourceScrollLines("down"))
		return model, nil
	}
	if name == "up" || name == "k" {
		model.sourceViewport.ScrollUp(model.sourceScrollLines("up"))
		return model, nil
	}
	if name == "home" || name == "g" {
		model.sourceViewport.GotoTop()
		return model, nil
	}
	if name == "end" || name == "G" {
		model.sourceViewport.GotoBottom()
		return model, nil
	}
	updated, command := model.sourceViewport.Update(key)
	model.sourceViewport = updated
	return model, command
}

func openFind(model *Model, source bool) (tea.Model, tea.Cmd) {
	model.findOpen = true
	model.findSource = source
	model.findInput.SetValue(model.findQuery)
	model.findInput.CursorEnd()
	model.findInput.Focus()
	return model, textinput.Blink
}

func handleFindKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if name == "esc" || name == "escape" {
		model.findOpen = false
		model.findInput.Blur()
		return model, nil
	}
	if name == "enter" {
		model.findQuery = model.findInput.Value()
		model.findOpen = false
		model.findInput.Blur()
		if model.findQuery == "" {
			model.status = ""
		} else {
			model.findNext(1)
		}
		return model, nil
	}
	updated, command := model.findInput.Update(key)
	model.findInput = updated
	return model, command
}

func findNext(model *Model, direction int) {
	query := strings.ToLower(model.findQuery)
	if query == "" {
		return
	}
	if model.findSource {
		lines := strings.Split(model.sourceSearchText, "\n")
		if len(lines) == 0 {
			return
		}
		start := model.sourceViewport.YOffset + direction
		for checked := 0; checked < len(lines); checked++ {
			line := ((start+checked*direction)%len(lines) + len(lines)) % len(lines)
			if strings.Contains(strings.ToLower(lines[line]), query) {
				model.sourceViewport.SetYOffset(line)
				model.status = ""
				return
			}
		}
		model.status = fmt.Sprintf("find: %q not found", model.findQuery)
		return
	}
	files := model.displayFiles()
	if len(files) == 0 {
		model.status = fmt.Sprintf("find: %q not found", model.findQuery)
		return
	}
	start := model.cursor + direction
	for checked := 0; checked < len(files); checked++ {
		index := ((start+checked*direction)%len(files) + len(files)) % len(files)
		if strings.Contains(strings.ToLower(files[index].Path), query) {
			model.cursor = index
			model.selectCursor()
			model.ensureVisible()
			model.status = ""
			return
		}
	}
	model.status = fmt.Sprintf("find: %q not found", model.findQuery)
}

const sourceRepeatWindow = 125 * time.Millisecond

func sourceScrollLines(model *Model, direction string) int {
	now := time.Now()
	lines := 1
	if model.sourceLastKey == direction && !model.sourceLastAt.IsZero() && now.Sub(model.sourceLastAt) <= sourceRepeatWindow {
		model.sourceRapid++
	} else {
		model.sourceRapid = 0
	}
	if model.sourceRapid >= 2 {
		lines = 2
	}
	model.sourceLastKey = direction
	model.sourceLastAt = now
	return lines
}

func handleColumnKey(model *Model, name string) (tea.Model, tea.Cmd) {
	items := columnNames()
	if isToggleKey(name) {
		key := items[model.columnCursor].key
		model.visible[key] = !model.visible[key]
		if !model.visible[key] && model.sortKey == key {
			model.sortKey = "score"
			model.sortReverse = true
			model.refreshDisplayFiles()
		}
		if key == "typesafety" || key == "nesting" || key == "coupling" {
			model.setColumnWeightEnabled(key, model.visible[key])
			model.rebuildWeightedDocument()
			model.restoreSelection()
		}
		model.clampPathOffset()
		model.persistUserPreferences()
		if key == "typesafety" {
			return model, model.syncTypeScriptTypes()
		}
		return model, nil
	}
	switch name {
	case "esc", "escape", "q":
		model.columns = false
		if model.columnsFromSettings {
			model.columnsFromSettings = false
			model.settings = true
		}
	case "up", "k":
		model.columnCursor = max(0, model.columnCursor-1)
	case "down", "j":
		model.columnCursor = min(len(items)-1, model.columnCursor+1)
	}
	return model, nil
}

func handleSortKey(model *Model, name string) (tea.Model, tea.Cmd) {
	switch name {
	case "esc", "escape", "q":
		model.sortOpen = false
	case "up", "k":
		model.moveSortCursor(-1)
	case "down", "j":
		model.moveSortCursor(1)
	case "left", "h":
		model.activateHighlightedSort(false, true)
	case "right", "l":
		model.activateHighlightedSort(true, true)
	case " ":
		model.activateHighlightedSort(false, false)
	}
	return model, nil
}

func prepareSortDirections(model *Model) {
	if model.sortDirections == nil {
		model.sortDirections = make(map[string]bool, len(sortFields()))
		for _, item := range sortFields() {
			model.sortDirections[item.key] = true
		}
	}
	model.sortDirections[model.sortKey] = model.sortReverse
}

func sortDirection(model Model, key string) bool {
	if direction, ok := model.sortDirections[key]; ok {
		return direction
	}
	if key == model.sortKey {
		return model.sortReverse
	}
	return true
}

func activateHighlightedSort(model *Model, direction bool, changeDirection bool) {
	if !model.sortOptionEnabled(model.sortCursor) {
		return
	}
	model.prepareSortDirections()
	key := sortFields()[model.sortCursor].key
	if changeDirection {
		model.sortDirections[key] = direction
	}
	model.sortKey = key
	model.sortReverse = model.sortDirections[key]
	model.refreshDisplayFiles()
	model.restoreSelection()
	model.persistUserPreferences()
}

func openSelectedFileInfo(model *Model) {
	if len(model.displayFiles()) > 0 {
		model.detail = true
	}
}

func handleKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return dispatchKey(model, key)
}

func move(model *Model, delta int) {
	files := model.displayFiles()
	if len(files) == 0 {
		return
	}
	model.cursor = min(len(files)-1, max(0, model.cursor+delta))
	model.selectCursor()
	model.ensureVisible()
}

func (model *Model) movePath(delta int) {
	model.pathOffset = min(model.maxPathOffset(), max(0, model.pathOffset+delta))
}

func (model *Model) clampPathOffset() {
	model.pathOffset = min(model.maxPathOffset(), max(0, model.pathOffset))
}

func maxPathOffset(model Model) int {
	viewportWidth := model.pathViewportWidth()
	if viewportWidth <= 0 {
		return 0
	}
	if model.displayFilesReady {
		return max(0, model.longestDisplayPath-viewportWidth)
	}
	longest := 0
	for _, file := range model.displayFiles() {
		longest = max(longest, lipgloss.Width(file.Path))
	}
	return max(0, longest-viewportWidth)
}

func selectCursor(model *Model) {
	files := model.displayFiles()
	if model.cursor >= 0 && model.cursor < len(files) {
		model.selected = files[model.cursor].Path
	}
}
