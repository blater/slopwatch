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

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case watcherReady:
		if message.err != nil {
			model.analyzing = false
			model.initialAnalysis = false
			model.status = message.err.Error()
			model.markFreshness(nil, report.FreshnessStaleError, "workspace verification could not start")
			return model, nil
		}
		model.markFreshness(nil, report.FreshnessVerifying, "validating current workspace")
		return model, tea.Batch(model.waitForChange(), model.analyze(nil, true))
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		model.ensureVisible()
		model.clampPathOffset()
		model.clampDetailOffset()
		if model.sourceView {
			model.resizeSourceViewport()
		}
		return model, nil
	case sourceChange:
		command := model.waitForChange()
		if message.Err != nil {
			model.status = message.Err.Error()
			return model, command
		}
		if message.Full {
			model.markFreshness(nil, report.FreshnessRefreshing, "workspace inputs changed")
			if model.analyzing {
				model.pendingFullAnalysis = true
				return model, command
			}
			model.analyzing = true
			return model, tea.Batch(command, model.analyze(nil, true))
		}
		model.markFreshness(message.Paths, report.FreshnessRefreshing, "source changed")
		for _, path := range message.Paths {
			model.queued[path] = true
			if state, exists := model.rows[path]; exists {
				state.editedAt = time.Now()
				state.direction = 0
				model.rows[path] = state
			}
		}
		if model.analyzing {
			return model, command
		}
		paths := model.takeQueue()
		model.analyzing = true
		return model, tea.Batch(command, model.analyzeExisting(paths))
	case analysisResult:
		wasInitial := model.initialAnalysis
		model.analyzing = false
		if message.full {
			model.initialAnalysis = false
		}
		if message.err != nil {
			model.status = message.err.Error()
			model.markFreshness(message.replace, report.FreshnessStaleError, "verification failed: "+message.err.Error())
		} else {
			model.status = ""
			model.merge(message)
			model.clampPathOffset()
			if wasInitial {
				if controller, ok := model.analyzer.(cacheReadController); ok {
					controller.SetCacheReads(true)
				}
			}
		}
		if model.pendingFullAnalysis {
			model.pendingFullAnalysis = false
			model.queued = map[string]bool{}
			model.analyzing = true
			return model, model.analyze(nil, true)
		}
		if len(model.queued) > 0 {
			paths := model.takeQueue()
			model.analyzing = true
			return model, model.analyzeExisting(paths)
		}
		return model, nil
	case animationTick:
		model.animationFrame++
		return model, tickAnimation(model.analyzing)
	case startupLogoExpired:
		model.startupLogoExpired = true
		return model, nil
	case sourceLoaded:
		if !model.sourceView || message.generation != model.sourceLoadGeneration || message.path != model.sourcePath {
			return model, nil
		}
		model.sourceViewport = message.viewport
		model.resizeSourceViewport()
		model.sourceSearchText = message.contents
		model.sourceLoading = false
		if message.highlight {
			width, height := model.sourceDimensions()
			return model, highlightSourceCommand(message.generation, message.path, message.contents, width, height)
		}
		return model, nil
	case sourceHighlighted:
		if !model.sourceView || message.generation != model.sourceLoadGeneration || message.path != model.sourcePath {
			return model, nil
		}
		message.viewport.SetYOffset(model.sourceViewport.YOffset)
		model.sourceViewport = message.viewport
		model.resizeSourceViewport()
		return model, nil
	case tea.KeyMsg:
		return model.handleKey(message)
	}
	return model, nil
}

func (model *Model) markFreshness(paths []string, freshness report.Freshness, note string) {
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

func (model Model) analyzeExisting(paths []string) tea.Cmd {
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

func (model *Model) takeQueue() []string {
	paths := make([]string, 0, len(model.queued))
	for path := range model.queued {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	model.queued = map[string]bool{}
	return paths
}

func (model *Model) mergeDocument(result analysisResult) {
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

func (model *Model) merge(result analysisResult) {
	oldScores := map[string]float64{}
	oldRanks := map[string]int{}
	for _, file := range model.document.Files {
		oldScores[file.Path] = file.Score
		oldRanks[file.Path] = file.Rank
	}
	model.mergeDocument(result)
	model.rebuildWeightedDocument()
	model.document.SortAndRank()
	now := time.Now()
	model.mergeRows(result, oldScores, oldRanks, now, result.full && len(oldScores) == 0)
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

func (model *Model) restoreSelection() {
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

func (model *Model) refreshDisplayFiles() {
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

func (model Model) displayFiles() []report.File {
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
		fields = append(fields, sortField{column: column})
	}
	fields = append(fields, sortField{column: column{key: "filename", title: "PATH", shortDescription: "filename", width: 1, defaultVisible: true}})
	return fields
}

func (model Model) sortOptionEnabled(index int) bool {
	field := sortFields()[index]
	return field.key == "score" || field.key == "filename" || model.visible[field.key]
}

func (model *Model) moveSortCursor(delta int) {
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

func (model Model) less(left, right report.File) bool {
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

func (model Model) sortValue(file report.File) (float64, bool) {
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

func (model *Model) handleDetailKey(name string) (tea.Model, tea.Cmd) {
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

func (model *Model) handleSourceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (model *Model) openFind(source bool) (tea.Model, tea.Cmd) {
	model.findOpen = true
	model.findSource = source
	model.findInput.SetValue(model.findQuery)
	model.findInput.CursorEnd()
	model.findInput.Focus()
	return model, textinput.Blink
}

func (model *Model) handleFindKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (model *Model) findNext(direction int) {
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

func (model *Model) sourceScrollLines(direction string) int {
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

func (model *Model) handleColumnKey(name string) (tea.Model, tea.Cmd) {
	items := columnNames()
	switch name {
	case "esc", "escape", "q", "enter":
		model.columns = false
		if model.columnsFromSettings {
			model.columnsFromSettings = false
			model.settings = true
		}
	case "up", "k":
		model.columnCursor = max(0, model.columnCursor-1)
	case "down", "j":
		model.columnCursor = min(len(items)-1, model.columnCursor+1)
	case " ":
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
		if key == "typesafety" {
			return model, model.syncTypeScriptTypes()
		}
	}
	return model, nil
}

func (model *Model) handleSortKey(name string) (tea.Model, tea.Cmd) {
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

func (model *Model) prepareSortDirections() {
	if model.sortDirections == nil {
		model.sortDirections = make(map[string]bool, len(sortFields()))
		for _, item := range sortFields() {
			model.sortDirections[item.key] = true
		}
	}
	model.sortDirections[model.sortKey] = model.sortReverse
}

func (model Model) sortDirection(key string) bool {
	if direction, ok := model.sortDirections[key]; ok {
		return direction
	}
	if key == model.sortKey {
		return model.sortReverse
	}
	return true
}

func (model *Model) activateHighlightedSort(direction bool, changeDirection bool) {
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
}

func (model *Model) openSelectedFileInfo() {
	if len(model.displayFiles()) > 0 {
		model.detail = true
	}
}

func (model *Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if model.findOpen {
		return model.handleFindKey(key)
	}
	if model.infoOpen {
		if model.handleDialogKey(name) {
			return model, nil
		}
		return model.handleInfoKey(name)
	}
	if model.help {
		return model.handleHelpKey(name)
	}
	if model.detail {
		return model.handleDetailKey(name)
	}
	if model.sourceView {
		if name == "f" || name == "/" {
			return model.openFind(true)
		}
		return model.handleSourceKey(key)
	}
	if model.columns {
		return model.handleColumnKey(name)
	}
	if model.sortOpen {
		return model.handleSortKey(name)
	}
	if model.weightsOpen {
		return model.handleWeightsKey(name)
	}
	if model.settings {
		return model.handleSettingsKey(name)
	}
	switch name {
	case "ctrl+c", "q":
		return model, tea.Quit
	case "up", "k":
		model.move(-1)
	case "down", "j":
		model.move(1)
	case "left":
		model.movePath(-pathScrollStep)
	case "right":
		model.movePath(pathScrollStep)
	case "ctrl+f", "pgdown":
		model.move(max(1, model.bodyHeight()))
	case "ctrl+b", "pgup":
		model.move(-max(1, model.bodyHeight()))
	case "home":
		model.cursor = 0
		model.selectCursor()
		model.ensureVisible()
	case "end":
		model.cursor = max(0, len(model.displayFiles())-1)
		model.selectCursor()
		model.ensureVisible()
	case "enter", "i":
		model.openSelectedFileInfo()
	case "v":
		return model, model.openSourceView()
	case "c":
		model.settings = true
		model.settingsCursor = 1
	case "s":
		model.settings = true
		model.settingsCursor = 0
	case "o":
		model.sortOpen = true
		model.prepareSortDirections()
		model.sortCursor = 0
		for index, item := range sortFields() {
			if item.key == model.sortKey && model.sortOptionEnabled(index) {
				model.sortCursor = index
				break
			}
		}
		if !model.sortOptionEnabled(model.sortCursor) {
			model.moveSortCursor(1)
		}
	case "h":
		model.help = true
		model.helpCursor = 0
	case "f", "/":
		return model.openFind(false)
	case "n":
		if model.findQuery != "" {
			model.findNext(1)
		}
	case "N":
		if model.findQuery != "" {
			model.findNext(-1)
		}
	case "r":
		if !model.analyzing {
			model.analyzing = true
			return model, model.analyze(nil, true)
		}
	}
	return model, nil
}

func (model *Model) move(delta int) {
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

func (model Model) maxPathOffset() int {
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

func (model *Model) selectCursor() {
	files := model.displayFiles()
	if model.cursor >= 0 && model.cursor < len(files) {
		model.selected = files[model.cursor].Path
	}
}
