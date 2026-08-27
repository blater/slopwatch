package follow

import (
	"fmt"
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopmochi/internal/scoring"
	"github.com/blater/slopmochi/internal/style"
)

type componentWeight struct {
	id       string
	label    string
	category string
	parent   string
	axis     string
	value    float64
}

var componentWeights = followComponentWeights()

func followComponentWeights() []componentWeight {
	definitions := scoring.Components()
	result := make([]componentWeight, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, componentWeight{
			id: definition.ID, label: definition.Label, category: definition.Category,
			parent: definition.Parent, axis: definition.Axis, value: definition.DefaultWeight,
		})
	}
	return result
}

func defaultWeights() map[string]float64 {
	return scoring.DefaultWeights()
}

func defaultWeightEnabled() map[string]bool {
	return scoring.DefaultEnabled()
}

func isWeightEnabled(model Model, id string) bool {
	return scoring.NewPolicy(model.weights, model.weightEnabled).Enabled(id)
}

func rebuildWeightedDocument(model *Model) {
	if len(model.baseDocument.Files) == 0 && len(model.document.Files) > 0 {
		model.baseDocument = model.document
	}
	document := scoring.ProjectDocument(
		model.baseDocument,
		scoring.NewPolicy(model.weights, model.weightEnabled),
	)
	model.document = document
	model.refreshFreshnessStatus()
	model.refreshDisplayFiles()
}

func defaultWeight(id string) float64 {
	return scoring.DefaultWeight(id)
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

type settingsItem struct {
	key   string
	label string
}

var settingsItems = []settingsItem{
	{key: "agents", label: "Agents"},
	{key: "appearance", label: "Appearance"},
	{key: "columns", label: "Columns"},
	{key: "concurrency", label: "Concurrency"},
	{key: "fix", label: "Fix defaults"},
	{key: "delivery", label: "Git & pull requests"},
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
		return model, openSetting(model, settingsItems[model.settingsCursor].key)
	}
	return model, nil
}

func openSetting(model *Model, key string) tea.Cmd {
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
	case "agents", "fix", "concurrency", "delivery":
		return model.openConfigSettings(configSettingsKind(key))
	}
	return nil
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
	if isToggleKey(name) {
		model.toggleWeight()
		return model, model.syncTypeScriptTypes()
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
	case "c":
		model.weightsResetConfirm = true
	case "i":
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
	content = scrollModalLines(content, model.settingsCursor, model.modalBodyHeight())
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
		body = append(body, style.ToggleValueOption(fmt.Sprintf("[%s]", mark), fmt.Sprintf("%5.1f", model.weights[item.id]), item.label, index == model.weightCursor, 52, 8))
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
