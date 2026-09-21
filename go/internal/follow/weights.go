package follow

import (
	"fmt"
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
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

func projectWeightedDocument(model *Model) {
	if len(model.files.BaseDocument.Files) == 0 && len(model.files.Document.Files) > 0 {
		model.files.BaseDocument = model.files.Document
	}
	document := scoring.ProjectDocument(
		model.files.BaseDocument,
		scoring.NewPolicy(model.weights, model.weightEnabled),
	)
	model.files.Document = document
	model.files.refreshFreshnessStatus()
	model.files.refreshDisplayFiles(model.options.Limit)
}

func projectWeightedFiles(model Model, files []report.File) []report.File {
	return scoring.ProjectFiles(files, scoring.NewPolicy(model.weights, model.weightEnabled))
}

func rebuildWeightedDocument(model *Model) {
	projectWeightedDocument(model)
	model.files.rebuildScoreDistribution()
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
	if model.files.Visible["typesafety"] {
		return true
	}
	for _, item := range componentWeights {
		if item.axis == "typescript_type_safety" && isWeightEnabled(model, item.id) {
			return true
		}
	}
	return false
}

func hasTypeScriptTypeData(model Model) bool {
	found := false
	for _, file := range model.files.BaseDocument.Files {
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
	enabled := typeScriptTypesWanted(*model)
	controller.SetTypeScriptTypes(enabled)
	if !enabled || hasTypeScriptTypeData(*model) {
		return nil
	}
	if model.analyzing {
		model.runtime.pendingFullAnalysis = true
		return nil
	}
	model.analyzing = true
	emit := beginAnalysisProgress(model, report.FreshnessVerifying, "analysis in progress")
	return analysisCommandWithProgress(model.analyzer, model.options.Targets, nil, true, emit)
}

type settingsItem struct {
	key   string
	label string
}

var settingsItems = []settingsItem{
	{key: "agents", label: "Agents"},
	{key: "appearance", label: "Appearance"},
	{key: "analysis", label: "Static Analysis"},
}

var settingsGroups = map[string][]settingsItem{
	"agents":     {{key: "agent-setup", label: "Agent Setup"}, {key: "fix", label: "Fix Settings"}, {key: "delivery", label: "Git Settings"}},
	"appearance": {{key: "theme", label: "Theme"}, {key: "columns", label: "Columns"}},
	"analysis":   {{key: "files", label: "Files"}, {key: "weights", label: "Weights"}},
}

func (model Model) currentSettingsItems() []settingsItem {
	if model.settingsGroup != "" {
		return settingsGroups[model.settingsGroup]
	}
	return settingsItems
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
	if model.runtime.filesSettings {
		if name == "up" || name == "k" || name == "down" || name == "j" || name == "tab" {
			model.runtime.filesCursor = 1 - model.runtime.filesCursor
			return model, nil
		}
		if isToggleKey(name) {
			if model.runtime.filesCursor == 1 {
				return model, model.openFilesExclusions()
			}
			return model, model.toggleGitignore()
		}
		if name == "esc" || name == "escape" || name == "q" {
			model.runtime.filesSettings = false
		}
		return model, nil
	}
	switch name {
	case "esc", "escape", "q", "s":
		if model.runtime.filesSettings {
			model.runtime.filesSettings = false
		} else if model.settingsGroup != "" {
			model.settingsGroup = ""
			model.settingsCursor = model.settingsRootCursor
		} else {
			model.settings = false
		}
	case "up", "k":
		model.settingsCursor = max(0, model.settingsCursor-1)
	case "down", "j":
		model.settingsCursor = min(len(model.currentSettingsItems())-1, model.settingsCursor+1)
	case "enter":
		return model, openSetting(model, model.currentSettingsItems()[model.settingsCursor].key)
	}
	return model, nil
}

func openSetting(model *Model, key string) tea.Cmd {
	if _, group := settingsGroups[key]; group {
		model.settingsGroup = key
		model.settingsRootCursor = settingsIndex(key)
		model.settingsCursor = 0
		model.settings = true
		return nil
	}
	model.settings = false
	switch key {
	case "files":
		model.settings = true
		model.runtime.filesSettings = true
		model.runtime.filesCursor = 0
	case "theme":
		model.appearance = true
		model.appearanceCursor = 0
		if model.theme == style.ThemeLight {
			model.appearanceCursor = 1
		}
	case "columns":
		model.columns = true
		model.runtime.columnsFromSettings = true
	case "weights":
		model.weightsOpen = true
		model.weightCursor = 0
		model.runtime.weightsResetConfirm = false
	case "agent-setup":
		return model.openConfigSettings(configAgents)
	case "fix", "concurrency", "delivery":
		return model.openConfigSettings(configSettingsKind(key))
	}
	return nil
}

func handleWeightsKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if model.runtime.weightsResetConfirm {
		switch name {
		case "y", "Y":
			resetAllWeights(model)
			model.runtime.weightsResetConfirm = false
			return model, model.syncTypeScriptTypes()
		case "n", "N", "esc", "escape":
			model.runtime.weightsResetConfirm = false
		}
		return model, nil
	}
	if isToggleKey(name) {
		toggleWeight(model)
		return model, model.syncTypeScriptTypes()
	}
	switch name {
	case "esc", "escape", "q":
		model.weightsOpen = false
		model.runtime.weightsResetConfirm = false
		model.settings = true
	case "up", "k":
		model.weightCursor = max(0, model.weightCursor-1)
	case "down", "j":
		model.weightCursor = min(len(componentWeights)-1, model.weightCursor+1)
	case "left", "h", "-":
		model.adjustWeight(-positiveOrDefault(model.weightStep, defaultWeightStep))
	case "right", "l", "+", "=":
		model.adjustWeight(positiveOrDefault(model.weightStep, defaultWeightStep))
	case "r":
		resetWeight(model)
		return model, model.syncTypeScriptTypes()
	case "c":
		model.runtime.weightsResetConfirm = true
	case "i":
		openInfo(model, weightInfoKey(componentWeights[model.weightCursor].id))
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
	rebuildWeightedDocument(model)
	restoreSelection(model)
	persistUserPreferences(model)
}

func resetAllWeights(model *Model) {
	for _, item := range componentWeights {
		model.weights[item.id] = item.value
		if model.weightEnabled == nil {
			model.weightEnabled = defaultWeightEnabled()
		}
		model.weightEnabled[item.id] = defaultWeightEnabled()[item.id]
	}
	rebuildWeightedDocument(model)
	restoreSelection(model)
	persistUserPreferences(model)
}

func toggleWeight(model *Model) {
	item := componentWeights[model.weightCursor]
	if model.weightEnabled == nil {
		model.weightEnabled = defaultWeightEnabled()
	}
	model.weightEnabled[item.id] = !isWeightEnabled(*model, item.id)
	rebuildWeightedDocument(model)
	restoreSelection(model)
	persistUserPreferences(model)
}

func (model *Model) adjustWeight(delta float64) {
	item := componentWeights[model.weightCursor]
	value := model.weights[item.id] + delta
	model.weights[item.id] = math.Max(0, math.Min(positiveOrDefault(model.maximumWeight, defaultMaximumWeight), value))
	rebuildWeightedDocument(model)
	restoreSelection(model)
	persistUserPreferences(model)
}

func settingsView(model Model) string {
	items := model.currentSettingsItems()
	content := make([]string, 0, len(items))
	for index, item := range items {
		content = append(content, style.ModalOption(item.label, index == model.settingsCursor, 34))
	}
	content = scrollModalLines(content, model.settingsCursor, model.modalBodyHeight())
	title := "SETTINGS"
	if model.settingsGroup != "" {
		title = settingsItems[settingsIndex(model.settingsGroup)].label
	}
	return style.Popup(style.Heading(title), content, "", 38)
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
		if isWeightEnabled(model, item.id) {
			mark = "✓"
		}
		body = append(body, style.ToggleValueOption(fmt.Sprintf("[%s]", mark), fmt.Sprintf("%5.1f", model.weights[item.id]), item.label, index == model.weightCursor, 52, 8))
	}
	content := scrollModalLines(body, selectedLine, max(1, model.modalBodyHeight()-1))
	footer := ""
	if model.runtime.weightsResetConfirm {
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
