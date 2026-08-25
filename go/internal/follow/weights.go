package follow

import (
	"fmt"
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

var componentWeights = []struct {
	id       string
	label    string
	category string
	parent   string
	axis     string
	value    float64
}{
	{"cognitive_complexity", "Cognitive complexity", "Structural", "COG", "structural_core", 10},
	{"cyclomatic_method_complexity", "Routine", "Structural", "CYCLO", "structural_core", 5},
	{"cyclomatic_class_complexity", "Type", "Structural", "CYCLO", "structural_language", 5},
	{"npath_complexity", "NPath complexity", "Structural", "NPATH", "structural_core", 8},
	{"deeply_nested_if", "Deep nesting", "Structural", "Nesting", "structural_core", 6},
	{"module_shallowness", "Module shallowness", "Structural", "SHALLOW", "structural_core", 5},
	{"coupling_between_objects", "Type coupling", "Structural", "Coupling", "structural_language", 10},
	{"god_class", "Responsibility concentration", "Structural", "GOD", "structural_language", 1},
	{"ambiguous_boolean_expression", "Ambiguous boolean", "Type safety", "", "typescript_type_safety", 4},
	{"explicit_any", "Explicit any", "Type safety", "", "typescript_type_safety", 3},
	{"non_exhaustive_union", "Non-exhaustive union", "Type safety", "", "typescript_type_safety", 8},
	{"unsafe_type_assertion", "Unsafe assertion", "Type safety", "", "typescript_type_safety", 5},
	{"unsafe_type_boundary", "Unsafe boundary", "Type safety", "", "typescript_type_safety", 10},
	{"unsafe_type_propagation", "Unsafe propagation", "Type safety", "", "typescript_type_safety", 4},
	{"unsafe_type_use", "Unsafe type use", "Type safety", "", "typescript_type_safety", 4},
}

func defaultWeights() map[string]float64 {
	weights := make(map[string]float64, len(componentWeights))
	for _, item := range componentWeights {
		weights[item.id] = item.value
	}
	return weights
}

func defaultWeightEnabled() map[string]bool {
	enabled := make(map[string]bool, len(componentWeights))
	for _, item := range componentWeights {
		enabled[item.id] = item.axis != "typescript_type_safety" && item.parent != "Nesting"
	}
	return enabled
}

func isWeightEnabled(model Model, id string) bool {
	if model.weightEnabled == nil {
		return defaultWeightEnabled()[id]
	}
	enabled, known := model.weightEnabled[id]
	if !known {
		return defaultWeightEnabled()[id]
	}
	return enabled
}

func rebuildWeightedDocument(model *Model) {
	if len(model.baseDocument.Files) == 0 && len(model.document.Files) > 0 {
		model.baseDocument = model.document
	}
	document := model.baseDocument
	document.Files = weightedFiles(model, model.baseDocument.Files)
	document.SortAndRank()
	model.document = document
	model.refreshFreshnessStatus()
	model.refreshDisplayFiles()
}

func weightedFiles(model *Model, originals []report.File) []report.File {
	files := make([]report.File, 0, len(originals))
	for _, original := range originals {
		files = append(files, weightedFile(model, original))
	}
	return files
}

func weightedFile(model *Model, original report.File) report.File {
	if len(original.Components) == 0 {
		return original
	}
	file := original
	file.Components = make(map[string]report.Component, len(original.Components))
	file.Axes = map[string]float64{}
	file.Score = 0
	for id, originalComponent := range original.Components {
		component, axis := weightedComponent(model, id, originalComponent)
		file.Components[id] = component
		file.Axes[axis] += component.Contribution
	}
	for axis, value := range file.Axes {
		file.Axes[axis] = roundWeight(value)
		file.Score += file.Axes[axis]
	}
	file.Score = roundWeight(file.Score)
	file.ValidZero = file.Complete && file.Score == 0
	return file
}

func weightedComponent(model *Model, id string, original report.Component) (report.Component, string) {
	component := original
	component.Subjects = append([]report.SubjectContribution(nil), original.Subjects...)
	factor := componentWeightFactor(model, id)
	component.Contribution = roundWeight(component.Contribution * factor)
	component.ObservedContribution = original.ObservedContribution
	for index := range component.Subjects {
		component.Subjects[index].Contribution = roundWeight(component.Subjects[index].Contribution * factor)
	}
	return component, componentAxis(id)
}

func componentWeightFactor(model *Model, id string) float64 {
	base := defaultWeight(id)
	weight := model.weights[id]
	if weight == 0 && base > 0 {
		if _, known := model.weights[id]; !known {
			weight = base
		}
	}
	if base <= 0 || !model.isWeightEnabled(id) {
		return 0
	}
	return weight / base
}

func defaultWeight(id string) float64 {
	for _, item := range componentWeights {
		if item.id == id {
			return item.value
		}
	}
	return 1
}

func componentAxis(id string) string {
	for _, item := range componentWeights {
		if item.id == id {
			return item.axis
		}
	}
	return "unknown"
}

func setColumnWeightEnabled(model *Model, columnKey string, enabled bool) {
	if model.weightEnabled == nil {
		model.weightEnabled = defaultWeightEnabled()
	}
	for _, item := range componentWeights {
		belongs := (columnKey == "typesafety" && item.axis == "typescript_type_safety") ||
			(columnKey == "nesting" && item.parent == "Nesting") ||
			(columnKey == "coupling" && item.id == "coupling_between_objects")
		if belongs {
			model.weightEnabled[item.id] = enabled
		}
	}
}

func typeScriptTypesWanted(model Model) bool {
	if model.options.TypeScriptTypes {
		return true
	}
	if model.visible["typesafety"] {
		return true
	}
	for _, item := range componentWeights {
		if item.axis == "typescript_type_safety" && model.isWeightEnabled(item.id) {
			return true
		}
	}
	return false
}

func hasTypeScriptTypeData(model Model) bool {
	found := false
	for _, file := range model.baseDocument.Files {
		if file.Language != "typescript" {
			continue
		}
		found = true
		if _, exists := file.Components["explicit_any"]; !exists {
			return false
		}
	}
	return found
}

func (model *Model) syncTypeScriptTypes() tea.Cmd {
	controller, supported := model.analyzer.(typeScriptTypesController)
	if !supported {
		return nil
	}
	enabled := model.typeScriptTypesWanted()
	controller.SetTypeScriptTypes(enabled)
	if !enabled || model.hasTypeScriptTypeData() {
		return nil
	}
	if model.analyzing {
		model.pendingFullAnalysis = true
		return nil
	}
	model.analyzing = true
	return model.analyze(nil, true)
}

func roundWeight(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}

type settingsItem struct {
	key   string
	label string
}

var settingsItems = []settingsItem{
	{key: "appearance", label: "Appearance"},
	{key: "columns", label: "Columns"},
	{key: "weights", label: "Weights"},
}

func settingsIndex(key string) int {
	for index, item := range settingsItems {
		if item.key == key {
			return index
		}
	}
	return 0
}

func handleSettingsKey(model *Model, name string) (tea.Model, tea.Cmd) {
	switch name {
	case "esc", "escape", "q", "s":
		model.settings = false
	case "up", "k":
		model.settingsCursor = max(0, model.settingsCursor-1)
	case "down", "j":
		model.settingsCursor = min(len(settingsItems)-1, model.settingsCursor+1)
	case "enter":
		openSetting(model, settingsItems[model.settingsCursor].key)
	}
	return model, nil
}

func openSetting(model *Model, key string) {
	model.settings = false
	switch key {
	case "appearance":
		model.appearance = true
		model.appearanceCursor = 0
		if model.theme == style.ThemeLight {
			model.appearanceCursor = 1
		}
	case "columns":
		model.columns = true
		model.columnsFromSettings = true
	case "weights":
		model.weightsOpen = true
		model.weightCursor = 0
		model.weightsResetConfirm = false
	}
}

func handleWeightsKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if model.weightsResetConfirm {
		switch name {
		case "y", "Y":
			model.resetAllWeights()
			model.weightsResetConfirm = false
			return model, model.syncTypeScriptTypes()
		case "n", "N", "esc", "escape":
			model.weightsResetConfirm = false
		}
		return model, nil
	}
	switch name {
	case "esc", "escape", "q":
		model.weightsOpen = false
		model.weightsResetConfirm = false
		model.settings = true
	case "up", "k":
		model.weightCursor = max(0, model.weightCursor-1)
	case "down", "j":
		model.weightCursor = min(len(componentWeights)-1, model.weightCursor+1)
	case "left", "h", "-":
		model.adjustWeight(-model.weightStepValue())
	case "right", "l", "+", "=":
		model.adjustWeight(model.weightStepValue())
	case "r":
		model.resetWeight()
		return model, model.syncTypeScriptTypes()
	case " ":
		model.toggleWeight()
		return model, model.syncTypeScriptTypes()
	case "c":
		model.weightsResetConfirm = true
	case "i", "enter":
		model.openInfo(weightInfoKey(componentWeights[model.weightCursor].id))
	}
	return model, nil
}

func resetWeight(model *Model) {
	item := componentWeights[model.weightCursor]
	model.weights[item.id] = defaultWeight(item.id)
	if model.weightEnabled == nil {
		model.weightEnabled = defaultWeightEnabled()
	}
	model.weightEnabled[item.id] = defaultWeightEnabled()[item.id]
	model.rebuildWeightedDocument()
	model.restoreSelection()
	model.persistUserPreferences()
}

func resetAllWeights(model *Model) {
	for _, item := range componentWeights {
		model.weights[item.id] = item.value
		if model.weightEnabled == nil {
			model.weightEnabled = defaultWeightEnabled()
		}
		model.weightEnabled[item.id] = defaultWeightEnabled()[item.id]
	}
	model.rebuildWeightedDocument()
	model.restoreSelection()
	model.persistUserPreferences()
}

func toggleWeight(model *Model) {
	item := componentWeights[model.weightCursor]
	if model.weightEnabled == nil {
		model.weightEnabled = defaultWeightEnabled()
	}
	model.weightEnabled[item.id] = !model.isWeightEnabled(item.id)
	model.rebuildWeightedDocument()
	model.restoreSelection()
	model.persistUserPreferences()
}

func (model *Model) adjustWeight(delta float64) {
	item := componentWeights[model.weightCursor]
	value := model.weights[item.id] + delta
	model.weights[item.id] = math.Max(0, math.Min(model.maximumWeightValue(), value))
	model.rebuildWeightedDocument()
	model.restoreSelection()
	model.persistUserPreferences()
}

func settingsView(model Model) string {
	content := make([]string, 0, len(settingsItems))
	for index, item := range settingsItems {
		content = append(content, style.ModalOption(item.label, index == model.settingsCursor, 34))
	}
	return style.Popup(style.Heading("SETTINGS"), content, "", 38)
}

func weightsView(model Model) string {
	body := []string{lipgloss.NewStyle().Bold(true).Foreground(style.TextPrimary).Render("  ENABLED     WEIGHT")}
	selectedLine := 0
	category := ""
	parent := ""
	for index, item := range componentWeights {
		if item.category != category {
			category = item.category
			parent = ""
			body = append(body, style.Heading(category))
		}
		if item.parent != parent && item.parent != "" {
			parent = item.parent
			body = append(body, lipgloss.NewStyle().Bold(true).Foreground(style.TextMuted).Render("  "+parent))
		}
		if index == model.weightCursor {
			selectedLine = len(body)
		}
		mark := "x"
		if model.isWeightEnabled(item.id) {
			mark = "✓"
		}
		body = append(body, style.ModalOption(fmt.Sprintf("    [%s]     %5.1f   %s", mark, model.weights[item.id], item.label), index == model.weightCursor, 52))
	}
	content := scrollModalLines(body, selectedLine, max(1, model.modalBodyHeight()-1))
	footer := ""
	if model.weightsResetConfirm {
		footer = hintRow(style.SurfaceModal, hintItem{"Y/N", "are you sure?"})
	} else {
		footer = hintRow(style.SurfaceModal,
			hintItem{"space", "on/off"},
			hintItem{"←/→", "weights"},
			hintItem{"r", "reset"},
			hintItem{"c", "clear"},
			hintItem{"i", "info"},
		)
	}
	return style.Popup(style.Heading("WEIGHTS"), content, footer, 56)
}

func (model Model) modalBodyHeight() int {
	if model.height <= 0 {
		return 1 << 30
	}
	return max(1, model.height-6)
}

func scrollModalLines(lines []string, selected, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	limit = max(1, limit)
	selected = min(len(lines)-1, max(0, selected))
	start := min(max(0, selected-limit+1), len(lines)-limit)
	return lines[start : start+limit]
}
