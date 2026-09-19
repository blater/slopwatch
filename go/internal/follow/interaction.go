package follow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const pathScrollStep = 4

func updateModel(model *Model, message tea.Msg) (tea.Model, tea.Cmd) {
	model.reconcileLegacyOverlayStack()
	beforeErrors := model.surfaceErrors()
	suspendErrorOverlay(model, message)
	updated, command := handleMessage(model, message)
	if result, ok := updated.(*Model); ok {
		result.reconcileLegacyOverlayStack()
		showChangedSurfaceErrors(result, beforeErrors)
		showStoredRuntimeError(result)
	}
	return updated, command
}

func markFreshness(model *Model, paths []string, freshness report.Freshness, note string) {
	wanted := pathSet(paths)
	model.files.ensureScoreDistribution()
	for index := range model.files.Document.Files {
		file := model.files.Document.Files[index]
		if len(wanted) == 0 || wanted[filepath.ToSlash(file.Path)] {
			updated := file
			updated.Freshness = freshness
			updateScoreDistributionFreshness(&model.files.ScoreDistribution, file, updated)
		}
	}
	changed := false
	for index := range model.files.BaseDocument.Files {
		file := &model.files.BaseDocument.Files[index]
		if len(wanted) == 0 || wanted[filepath.ToSlash(file.Path)] {
			file.Freshness = freshness
			file.FreshnessNote = note
			changed = true
		}
	}
	if changed {
		projectWeightedDocument(model)
	}
}

func pathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[filepath.ToSlash(path)] = true
	}
	return result
}

func mergeDocument(model *Model, result analysisResult) {
	if len(model.files.BaseDocument.Files) == 0 && len(model.files.Document.Files) > 0 {
		model.files.BaseDocument = model.files.Document
	}
	if result.full {
		model.files.BaseDocument = result.document
		return
	}
	replace := map[string]bool{}
	for _, path := range result.replace {
		replace[filepath.ToSlash(path)] = true
	}
	for _, file := range result.document.Files {
		replace[filepath.ToSlash(file.Path)] = true
	}
	kept := make([]report.File, 0, len(model.files.BaseDocument.Files)+len(result.document.Files))
	for _, file := range model.files.BaseDocument.Files {
		if !replace[file.Path] {
			kept = append(kept, file)
		}
	}
	model.files.BaseDocument.Files = append(kept, result.document.Files...)
	model.files.BaseDocument.Diagnostics = mergeDiagnostics(model.files.BaseDocument.Diagnostics, result.document.Diagnostics, replace, result.document.ExecutionPlans)
	model.files.BaseDocument.ExecutionPlans = mergeExecutionPlans(model.files.BaseDocument.ExecutionPlans, result.document.ExecutionPlans)
}

func mergeDiagnostics(previous, updated []map[string]any, affected map[string]bool, updatedPlans []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(previous)+len(updated))
	seen := map[string]bool{}
	replacedUnits := executionPlanIDs(updatedPlans)
	for _, diagnostic := range previous {
		path, hasPath := diagnostic["path"].(string)
		unitID, hasUnit := diagnostic["unit_id"].(string)
		if (hasPath && affected[filepath.ToSlash(path)]) || (hasUnit && replacedUnits[unitID]) {
			continue
		}
		appendDiagnostic(&merged, seen, diagnostic)
	}
	for _, diagnostic := range updated {
		appendDiagnostic(&merged, seen, diagnostic)
	}
	return merged
}

func executionPlanIDs(plans []map[string]any) map[string]bool {
	ids := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if unitID, _ := plan["unit_id"].(string); unitID != "" {
			ids[unitID] = true
		}
	}
	return ids
}

func appendDiagnostic(merged *[]map[string]any, seen map[string]bool, diagnostic map[string]any) {
	identityValues := make(map[string]any, len(diagnostic))
	for name, value := range diagnostic {
		if name != "invocation_id" {
			identityValues[name] = value
		}
	}
	identity := string(metadataIdentity(identityValues))
	if seen[identity] {
		return
	}
	seen[identity] = true
	*merged = append(*merged, diagnostic)
}

func mergeExecutionPlans(previous, updated []map[string]any) []map[string]any {
	if len(updated) == 0 {
		return previous
	}
	merged := append([]map[string]any(nil), previous...)
	indexes := make(map[string]int, len(merged))
	identities := make(map[string]bool, len(merged))
	for index, plan := range merged {
		if unitID, _ := plan["unit_id"].(string); unitID != "" {
			indexes[unitID] = index
		} else {
			identities[string(metadataIdentity(plan))] = true
		}
	}
	for _, plan := range updated {
		unitID, _ := plan["unit_id"].(string)
		if unitID == "" {
			identity := string(metadataIdentity(plan))
			if !identities[identity] {
				identities[identity] = true
				merged = append(merged, plan)
			}
			continue
		}
		if index, exists := indexes[unitID]; exists {
			merged[index] = plan
		} else {
			indexes[unitID] = len(merged)
			merged = append(merged, plan)
		}
	}
	return merged
}

func metadataIdentity(value map[string]any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(fmt.Sprintf("%#v", value))
	}
	return encoded
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
	model.files.ensureScoreDistribution()
	affected := map[string]bool{}
	if !result.full {
		for _, path := range result.replace {
			affected[filepath.ToSlash(path)] = true
		}
		for _, file := range result.document.Files {
			affected[filepath.ToSlash(file.Path)] = true
		}
	}
	oldAffected := map[string]report.File{}
	for _, file := range model.files.Document.Files {
		oldScores[file.Path] = file.Score
		oldRanks[file.Path] = file.Rank
		if affected[filepath.ToSlash(file.Path)] {
			oldAffected[filepath.ToSlash(file.Path)] = file
		}
	}
	mergeDocument(model, result)
	// Keep the projected copy synchronized before rebuilding weights. This is
	// also required when an incremental result removes its final affected row.
	model.files.Document = model.files.BaseDocument
	projectWeightedDocument(model)
	if result.full {
		model.files.rebuildScoreDistribution()
	} else {
		model.files.replaceScoreDistribution(oldAffected, projectWeightedFiles(*model, result.document.Files), affected)
	}
	model.files.Document.SortAndRank()
	model.files.pruneMarks()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
	now := time.Now()
	mergeRows(model, result, oldScores, oldRanks, now, result.full && len(oldScores) == 0)
	model.files.refreshDisplayFiles(model.options.Limit)
	restoreSelection(model)
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
	model.files.restoreSelection(model.options.Limit, bodyHeight(model.mainView, model.height))
}

func refreshDisplayFiles(model *Model) {
	model.files.refreshDisplayFiles(model.options.Limit)
}

func displayFiles(model Model) []report.File {
	return model.files.displayFiles(model.options.Limit)
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
	return model.files.sortOptionEnabled(index)
}

func moveSortCursor(model *Model, delta int) {
	model.files.moveSortCursor(delta)
}

func less(model Model, left, right report.File) bool {
	return filesLess(model.files.SortKey, model.files.SortReverse, left, right)
}

func sortValue(model Model, file report.File) (float64, bool) {
	return filesSortValue(model.files.SortKey, file)
}

func handleDetailKey(model *Model, name string) (tea.Model, tea.Cmd) {
	switch name {
	case "esc", "escape", "q":
		model.detail = false
		model.detailOffset = 0
	case "up", "k":
		model.detailOffset = max(0, model.detailOffset-1)
	case "down", "j":
		model.detailOffset = min(detailMaxOffset(*model), model.detailOffset+1)
	case "ctrl+f", "pgdown":
		model.detailOffset = min(detailMaxOffset(*model), model.detailOffset+max(1, detailBodyHeight(*model)-1))
	case "ctrl+b", "pgup":
		model.detailOffset = max(0, model.detailOffset-max(1, detailBodyHeight(*model)-1))
	case "home", "g":
		model.detailOffset = 0
	case "end", "G":
		model.detailOffset = detailMaxOffset(*model)
	}
	return model, nil
}

func handleSourceKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if name == "esc" || name == "escape" || name == "q" || name == "v" {
		model.source.close()
		return model, nil
	}
	if name == "ctrl+f" {
		model.source.viewport.PageDown()
		return model, nil
	}
	if name == "ctrl+b" {
		model.source.viewport.PageUp()
		return model, nil
	}
	if name == "n" && model.source.findQuery != "" {
		findNext(model, 1)
		return model, nil
	}
	if name == "N" && model.source.findQuery != "" {
		findNext(model, -1)
		return model, nil
	}
	if name == "down" || name == "j" {
		model.source.viewport.ScrollDown(model.source.scrollLines("down", time.Now()))
		return model, nil
	}
	if name == "up" || name == "k" {
		model.source.viewport.ScrollUp(model.source.scrollLines("up", time.Now()))
		return model, nil
	}
	if name == "home" || name == "g" {
		model.source.viewport.GotoTop()
		return model, nil
	}
	if name == "end" || name == "G" {
		model.source.viewport.GotoBottom()
		return model, nil
	}
	updated, command := model.source.viewport.Update(key)
	model.source.viewport = updated
	return model, command
}

func openFind(model *Model, source bool) (tea.Model, tea.Cmd) {
	model.source.findOpen = true
	model.source.findSource = source
	model.source.findInput.SetValue(model.source.findQuery)
	model.source.findInput.CursorEnd()
	model.source.findInput.Focus()
	return model, textinput.Blink
}

func handleFindKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if name == "esc" || name == "escape" {
		model.source.findOpen = false
		model.source.findInput.Blur()
		return model, nil
	}
	if name == "enter" {
		model.source.findQuery = model.source.findInput.Value()
		model.source.findOpen = false
		model.source.findInput.Blur()
		if model.source.findQuery == "" {
			model.status = ""
		} else {
			findNext(model, 1)
		}
		return model, nil
	}
	updated, command := model.source.findInput.Update(key)
	model.source.findInput = updated
	return model, command
}

func findNext(model *Model, direction int) {
	query := strings.ToLower(model.source.findQuery)
	if query == "" {
		return
	}
	if model.source.findSource {
		if model.source.findNext(direction) {
			model.status = ""
			return
		}
		model.status = fmt.Sprintf("find: %q not found", model.source.findQuery)
		return
	}
	files := model.files.displayFiles(model.options.Limit)
	if len(files) == 0 {
		model.status = fmt.Sprintf("find: %q not found", model.source.findQuery)
		return
	}
	start := model.files.Cursor + direction
	for checked := 0; checked < len(files); checked++ {
		index := ((start+checked*direction)%len(files) + len(files)) % len(files)
		if strings.Contains(strings.ToLower(files[index].Path), query) {
			model.files.Cursor = index
			model.files.selectCursor(model.options.Limit)
			model.ensureVisible()
			model.status = ""
			return
		}
	}
	model.status = fmt.Sprintf("find: %q not found", model.source.findQuery)
}

func handleColumnKey(model *Model, name string) (tea.Model, tea.Cmd) {
	items := columnNames()
	if isToggleKey(name) {
		key := items[model.columnCursor].key
		model.files.Visible[key] = !model.files.Visible[key]
		if !model.files.Visible[key] && model.files.SortKey == key {
			model.files.SortKey = "score"
			model.files.SortReverse = true
			model.files.refreshDisplayFiles(model.options.Limit)
		}
		if key == "typesafety" || key == "nesting" || key == "coupling" {
			setColumnWeightEnabled(model, key, model.files.Visible[key])
			rebuildWeightedDocument(model)
			restoreSelection(model)
		}
		model.clampPathOffset()
		persistUserPreferences(model)
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
		model.files.moveSortCursor(-1)
	case "down", "j":
		model.files.moveSortCursor(1)
	case "left", "h":
		activateHighlightedSort(model, false, true)
	case "right", "l":
		activateHighlightedSort(model, true, true)
	case " ":
		activateHighlightedSort(model, false, false)
	}
	return model, nil
}

func prepareSortDirections(model *Model) {
	model.files.prepareSortDirections()
}

func sortDirection(model Model, key string) bool {
	return model.files.sortDirection(key)
}

func activateHighlightedSort(model *Model, direction bool, changeDirection bool) {
	if !model.files.sortOptionEnabled(model.files.SortCursor) {
		return
	}
	model.files.activateSort(direction, changeDirection)
	model.files.refreshDisplayFiles(model.options.Limit)
	restoreSelection(model)
	persistUserPreferences(model)
}

func openSelectedFileInfo(model *Model) {
	if len(model.files.displayFiles(model.options.Limit)) > 0 {
		model.detail = true
	}
}

func handleKey(model *Model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Every key is user activity, including keys consumed by an overlay or
	// ignored by the current view. Reset before dispatch so selection highlight
	// cannot remain faded after an interaction.
	model.cursorActivity = time.Now()
	return dispatchKey(model, key)
}

func move(model *Model, delta int) {
	model.files.move(delta, model.options.Limit)
	model.ensureVisible()
}

func (model *Model) movePath(delta int) {
	model.files.HorizontalOffset = min(maxPathOffset(*model), max(0, model.files.HorizontalOffset+delta))
}

func (model *Model) clampPathOffset() {
	model.files.HorizontalOffset = min(maxPathOffset(*model), max(0, model.files.HorizontalOffset))
}

func maxPathOffset(model Model) int {
	return model.files.maxPathOffset(model.pathViewportWidth(), model.options.Limit)
}

func selectCursor(model *Model) {
	model.files.selectCursor(model.options.Limit)
}
func analyzeExisting(model Model, paths []string) tea.Cmd {
	if analyzer, ok := model.analyzer.(changeAnalyzer); ok {
		return func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			document, affected, err := analyzer.AnalyzeChanges(ctx, paths)
			if errors.Is(err, native.ErrIncrementalPlanUnavailable) {
				return analysisCommand(model.analyzer, model.options.Targets, nil, true)()
			}
			return analysisResult{document: document, replace: affected, paths: append([]string(nil), paths...), err: err}
		}
	}
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(filepath.Join(model.options.Workspace, filepath.FromSlash(path))); err == nil && !info.IsDir() {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return func() tea.Msg { return analysisResult{replace: paths, document: report.Document{}} }
	}
	command := analysisCommand(model.analyzer, model.options.Targets, existing, false)
	return func() tea.Msg {
		result := command()
		analysis := result.(analysisResult)
		analysis.replace = paths
		analysis.paths = append([]string(nil), paths...)
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
