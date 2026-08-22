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

	"github.com/blater/slopwatch/internal/report"
)

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		model.ensureVisible()
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
		model.analyzing = false
		if message.err != nil {
			model.status = message.err.Error()
		} else {
			model.status = ""
			model.merge(message)
		}
		if len(model.queued) > 0 {
			paths := model.takeQueue()
			model.analyzing = true
			return model, model.analyzeExisting(paths)
		}
		return model, nil
	case animationTick:
		model.animationFrame++
		model.expireTrends(time.Time(message))
		return model, tickAnimation()
	case tea.KeyMsg:
		return model.handleKey(message)
	}
	return model, nil
}

func (model *Model) expireTrends(now time.Time) {
	cutoff := now.Add(-model.options.TrendWindow)
	for path, state := range model.rows {
		first := 0
		for first+1 < len(state.ranks) && state.ranks[first].at.Before(cutoff) {
			first++
		}
		state.ranks = state.ranks[first:]
		if !state.scoreChangedAt.IsZero() && now.Sub(state.scoreChangedAt) > model.options.TrendWindow {
			state.scoreChangedAt = time.Time{}
			state.movementDelta = 0
		}
		model.rows[path] = state
	}
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
	if result.full {
		model.document = result.document
		return
	}
	replace := map[string]bool{}
	for _, path := range result.replace {
		replace[filepath.ToSlash(path)] = true
	}
	kept := make([]report.File, 0, len(model.document.Files)+len(result.document.Files))
	for _, file := range model.document.Files {
		if !replace[file.Path] {
			kept = append(kept, file)
		}
	}
	model.document.Files = append(kept, result.document.Files...)
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

func (model Model) displayFiles() []report.File {
	files := append([]report.File(nil), model.document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return model.less(files[left], files[right])
	})
	if model.options.Limit > 0 && len(files) > model.options.Limit {
		return files[:model.options.Limit]
	}
	return files
}

type sortField struct {
	key   string
	label string
}

func sortFields() []sortField {
	return []sortField{
		{"score", "Score"}, {"cog", "COG"},
		{"npath", "NPath"}, {"cyclo", "Cyclomatic"}, {"deep", "Shallowness"},
		{"god", "God"}, {"filename", "Filename"},
	}
}

func (model Model) less(left, right report.File) bool {
	if model.sortKey == "filename" {
		leftName := strings.ToLower(filepath.Base(left.Path))
		rightName := strings.ToLower(filepath.Base(right.Path))
		comparison := strings.Compare(leftName, rightName)
		if comparison == 0 {
			comparison = strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path))
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
	case "cog", "npath", "cyclo", "deep", "god":
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
	case "up", "k":
		model.columnCursor = max(0, model.columnCursor-1)
	case "down", "j":
		model.columnCursor = min(len(items)-1, model.columnCursor+1)
	case " ":
		key := items[model.columnCursor].key
		model.visible[key] = !model.visible[key]
	}
	return model, nil
}

func (model *Model) handleSortKey(name string) (tea.Model, tea.Cmd) {
	items := sortFields()
	switch name {
	case "esc", "escape", "q":
		model.sortOpen = false
	case "enter":
		model.sortKey = items[model.sortCursor].key
		model.sortReverse = model.pendingSort
		if _, exists := model.visible[model.sortKey]; exists && !model.options.Compact {
			model.visible[model.sortKey] = true
		}
		model.sortOpen = false
		model.restoreSelection()
	case "up", "k":
		model.sortCursor = max(0, model.sortCursor-1)
	case "down", "j":
		model.sortCursor = min(len(items)-1, model.sortCursor+1)
	case "left", "h":
		model.pendingSort = false
	case "right", "l":
		model.pendingSort = true
	case " ":
		model.pendingSort = !model.pendingSort
	}
	return model, nil
}

func (model *Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if model.findOpen {
		return model.handleFindKey(key)
	}
	if model.help {
		if name == "esc" || name == "escape" || name == "q" || name == "h" {
			model.help = false
		}
		return model, nil
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
	switch name {
	case "ctrl+c", "q":
		return model, tea.Quit
	case "up", "k":
		model.move(-1)
	case "down", "j":
		model.move(1)
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
	case "enter":
		if len(model.displayFiles()) > 0 {
			model.detail = true
		}
	case "v":
		model.openSourceView()
	case "c":
		model.columns = true
	case "s":
		model.sortOpen = true
		model.pendingSort = model.sortReverse
		for index, item := range sortFields() {
			if item.key == model.sortKey {
				model.sortCursor = index
				break
			}
		}
	case "h":
		model.help = true
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

func (model *Model) selectCursor() {
	files := model.displayFiles()
	if model.cursor >= 0 && model.cursor < len(files) {
		model.selected = files[model.cursor].Path
	}
}
