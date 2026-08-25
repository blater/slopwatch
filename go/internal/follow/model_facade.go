package follow

import (
	"time"

	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The Model methods below are the small public/test-facing façade. Their
// implementations live in focused functions so Model remains a state holder,
// rather than the owner of every piece of follow-mode behavior.

func (model Model) Init() tea.Cmd { return initModel(model) }

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	return updateModel(model, message)
}

func (model Model) View() string { return view(model) }

func (model Model) columnsView() string { return columnsView(model) }

func (model Model) sortView() string { return sortView(model) }

func (model Model) helpView() string { return helpView(model) }

func (model Model) detailView() string { return detailView(model) }

func (model Model) detailContent(file report.File, width int) []string {
	return detailContent(model, file, width)
}

func (model Model) detailDimensions() (int, int, int, int, int) {
	return detailDimensions(model)
}

func (model Model) detailBodyHeight() int { return detailBodyHeight(model) }

func (model Model) detailMaxOffset() int { return detailMaxOffset(model) }

func (model Model) activeDialogPolicy() dialogPolicy { return activeDialogPolicy(model) }

func (model *Model) handleDialogKey(name string) bool {
	return handleDialogKey(model, name)
}

func (model *Model) openInfo(key string) { openInfo(model, key) }

func (model *Model) handleInfoKey(name string) (tea.Model, tea.Cmd) {
	return handleInfoKey(model, name)
}

func (model *Model) handleHelpKey(name string) (tea.Model, tea.Cmd) {
	return handleHelpKey(model, name)
}

func (model Model) infoView() string { return infoView(model) }

func (model *Model) mergeRowState(file report.File, state rowState, result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) rowState {
	return mergeRowState(model, file, state, result, oldScores, oldRanks, now, baseline)
}

func (model Model) tableView() string { return tableView(model) }

func (model Model) freshnessStatus() string { return freshnessStatus(model) }

func (model Model) footer() string { return footer(model) }

func (model Model) activeColumns() []column { return activeColumns(model) }

func (model Model) header() string { return header(model) }

func (model Model) sortColumnVisible() bool { return sortColumnVisible(model) }

func (model Model) sortTitle() string { return sortTitle(model) }

func (model Model) sortIndicator() string { return sortIndicator(model) }

func (model Model) overlayAt(base, modal string, minimumTop int) string {
	return overlayAt(model, base, modal, minimumTop)
}

func (model Model) isWeightEnabled(id string) bool { return isWeightEnabled(model, id) }

func (model *Model) rebuildWeightedDocument() { rebuildWeightedDocument(model) }

func (model *Model) setColumnWeightEnabled(columnKey string, enabled bool) {
	setColumnWeightEnabled(model, columnKey, enabled)
}

func (model Model) typeScriptTypesWanted() bool { return typeScriptTypesWanted(model) }

func (model Model) hasTypeScriptTypeData() bool { return hasTypeScriptTypeData(model) }

func (model *Model) handleSettingsKey(name string) (tea.Model, tea.Cmd) {
	return handleSettingsKey(model, name)
}

func (model *Model) handleWeightsKey(name string) (tea.Model, tea.Cmd) {
	return handleWeightsKey(model, name)
}

func (model *Model) resetWeight() { resetWeight(model) }

func (model *Model) resetAllWeights() { resetAllWeights(model) }

func (model *Model) toggleWeight() { toggleWeight(model) }

func (model Model) settingsView() string { return settingsView(model) }

func (model Model) weightsView() string { return weightsView(model) }

func (model *Model) mergeRows(result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) {
	mergeRows(model, result, oldScores, oldRanks, now, baseline)
}

func (model Model) startupOverlay(base, logo string) string {
	return startupOverlay(model, base, logo)
}

func (model Model) startupView(base string) string { return startupView(model, base) }

func (model *Model) markFreshness(paths []string, freshness report.Freshness, note string) {
	markFreshness(model, paths, freshness, note)
}

func (model Model) analyzeExisting(paths []string) tea.Cmd {
	return analyzeExisting(model, paths)
}

func (model *Model) takeQueue() []string { return takeQueue(model) }

func (model *Model) mergeDocument(result analysisResult) { mergeDocument(model, result) }

func (model *Model) merge(result analysisResult) { merge(model, result) }

func (model *Model) restoreSelection() { restoreSelection(model) }

func (model *Model) refreshDisplayFiles() { refreshDisplayFiles(model) }

func (model Model) displayFiles() []report.File { return displayFiles(model) }

func (model Model) sortOptionEnabled(index int) bool {
	return sortOptionEnabled(model, index)
}

func (model *Model) moveSortCursor(delta int) { moveSortCursor(model, delta) }

func (model Model) less(left, right report.File) bool { return less(model, left, right) }

func (model Model) sortValue(file report.File) (float64, bool) {
	return sortValue(model, file)
}

func (model *Model) handleDetailKey(name string) (tea.Model, tea.Cmd) {
	return handleDetailKey(model, name)
}

func (model *Model) handleSourceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return handleSourceKey(model, key)
}

func (model *Model) openFind(source bool) (tea.Model, tea.Cmd) {
	return openFind(model, source)
}

func (model *Model) handleFindKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return handleFindKey(model, key)
}

func (model *Model) findNext(direction int) { findNext(model, direction) }

func (model *Model) sourceScrollLines(direction string) int {
	return sourceScrollLines(model, direction)
}

func (model *Model) handleColumnKey(name string) (tea.Model, tea.Cmd) {
	return handleColumnKey(model, name)
}

func (model *Model) handleSortKey(name string) (tea.Model, tea.Cmd) {
	return handleSortKey(model, name)
}

func (model *Model) prepareSortDirections() { prepareSortDirections(model) }

func (model Model) sortDirection(key string) bool { return sortDirection(model, key) }

func (model *Model) activateHighlightedSort(direction bool, changeDirection bool) {
	activateHighlightedSort(model, direction, changeDirection)
}

func (model *Model) openSelectedFileInfo() { openSelectedFileInfo(model) }

func (model *Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return handleKey(model, key)
}

func (model *Model) move(delta int) { move(model, delta) }

func (model Model) maxPathOffset() int { return maxPathOffset(model) }

func (model *Model) selectCursor() { selectCursor(model) }

func (model Model) renderRow(file report.File, selected bool) string {
	return renderRow(model, file, selected)
}

func (model Model) renderFixedColumns(file report.File, state rowState, background lipgloss.Color) string {
	return renderFixedColumns(model, file, state, background)
}

func (model Model) rowMarker(file report.File, state rowState, now time.Time) (string, lipgloss.Color) {
	return rowMarker(model, file, state, now)
}

func (model *Model) openSourceView() tea.Cmd { return openSourceView(model) }

func (model Model) sourceViewView() string { return sourceViewView(model) }
