package follow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/blater/slopmochi/internal/preferences"
	"github.com/blater/slopmochi/internal/report"
	"github.com/blater/slopmochi/internal/style"
)

type preferenceAnalyzer struct {
	typeScriptTypes bool
}

func (analyzer *preferenceAnalyzer) Analyze(context.Context, []string, []string) (report.Document, error) {
	return report.Document{}, nil
}

func (analyzer *preferenceAnalyzer) SetTypeScriptTypes(enabled bool) {
	analyzer.typeScriptTypes = enabled
}

func TestNewLoadsPreferencesAndCommandLineTrendOverride(t *testing.T) {
	ConfigureTerminalColours()
	t.Cleanup(ConfigureTerminalColours)
	workspace := t.TempDir()
	path := filepath.Join(t.TempDir(), "preferences.toml")
	value := defaultUserPreferences()
	value.Appearance.Theme = "light"
	value.Table.VisibleColumns = []string{"cog"}
	value.Table.SortBy = "filename"
	value.Table.SortDescending = false
	value.Interaction.TrendWindow = "42m"
	value.Scoring.WeightStep = 0.25
	value.Scoring.MaximumWeight = 12
	component := value.Scoring.Components["cognitive_complexity"]
	component.Weight = 3
	value.Scoring.Components["cognitive_complexity"] = component
	typeSafety := value.Scoring.Components["explicit_any"]
	typeSafety.Enabled = true
	value.Scoring.Components["explicit_any"] = typeSafety
	if err := preferences.Save(path, value); err != nil {
		t.Fatal(err)
	}

	analyzer := &preferenceAnalyzer{}
	model, err := New(report.Document{}, analyzer, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	if model.theme != style.ThemeLight || string(style.SurfaceScreen) != "#f7fafc" {
		t.Fatalf("theme was not loaded: model=%q surface=%q", model.theme, style.SurfaceScreen)
	}
	if len(model.visible) != 1 || !model.visible["cog"] || model.sortKey != "filename" || model.sortReverse {
		t.Fatalf("table preferences were not loaded: visible=%v sort=%s reverse=%t", model.visible, model.sortKey, model.sortReverse)
	}
	if model.options.TrendWindow != 42*time.Minute || model.weightStep != 0.25 || model.maximumWeight != 12 {
		t.Fatalf("tuning preferences were not loaded: trend=%s step=%v max=%v", model.options.TrendWindow, model.weightStep, model.maximumWeight)
	}
	if model.weights["cognitive_complexity"] != 3 || !analyzer.typeScriptTypes {
		t.Fatalf("scoring preferences were not loaded: weight=%v types=%t", model.weights["cognitive_complexity"], analyzer.typeScriptTypes)
	}

	overridden, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path, TrendWindow: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer overridden.Close()
	if overridden.options.TrendWindow != 2*time.Minute {
		t.Fatalf("command-line trend override = %s", overridden.options.TrendWindow)
	}
	overridden.appearanceCursor = 1
	overridden.selectAppearance()
	reloaded, err := preferences.LoadOrCreate(path, defaultUserPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Interaction.TrendWindow != "42m" {
		t.Fatalf("command-line override leaked into saved preferences: %q", reloaded.Interaction.TrendWindow)
	}
}

func TestPreferenceChangesSurviveModelRestart(t *testing.T) {
	ConfigureTerminalColours()
	t.Cleanup(ConfigureTerminalColours)
	workspace := t.TempDir()
	path := filepath.Join(t.TempDir(), "preferences.toml")
	model, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	model.appearanceCursor = 1
	model.selectAppearance()
	model.weightCursor = componentIndex("cognitive_complexity")
	model.adjustWeight(model.weightStepValue())
	model.toggleWeight()
	model.columnCursor = columnIndex("cog")
	model.handleColumnKey(" ")
	model.sortCursor = len(sortFields()) - 1
	model.activateHighlightedSort(false, true)
	model.Close()

	restarted, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.theme != style.ThemeLight {
		t.Fatalf("restarted theme = %q", restarted.theme)
	}
	if restarted.weights["cognitive_complexity"] != 10.5 {
		t.Fatalf("restarted weight = %v", restarted.weights["cognitive_complexity"])
	}
	if restarted.isWeightEnabled("cognitive_complexity") {
		t.Fatal("restarted model restored a disabled component")
	}
	if restarted.visible["cog"] {
		t.Fatal("restarted model restored a hidden column")
	}
	if restarted.sortKey != "filename" || restarted.sortReverse {
		t.Fatalf("restarted sort = %s reverse=%t", restarted.sortKey, restarted.sortReverse)
	}
}

func TestNewRecoversSemanticallyInvalidPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	value := defaultUserPreferences()
	value.Scoring.Components["cognitive_complexity"] = preferences.ComponentPreference{Enabled: true, Weight: 21}
	if err := preferences.Save(path, value); err != nil {
		t.Fatal(err)
	}
	model, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: t.TempDir(), Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	if model.preferences.Scoring.Components["cognitive_complexity"].Weight > model.preferences.Scoring.MaximumWeight {
		t.Fatalf("invalid preference was retained: %#v", model.preferences.Scoring)
	}
}

func TestCommandLineTypeScriptAnalysisOverridesDisabledPreference(t *testing.T) {
	analyzer := &preferenceAnalyzer{}
	model, err := New(report.Document{}, analyzer, Options{
		Workspace: t.TempDir(), Targets: []string{"."}, TypeScriptTypes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	if !analyzer.typeScriptTypes {
		t.Fatal("command-line TypeScript analysis was disabled by dashboard preferences")
	}
	model.syncTypeScriptTypes()
	if !analyzer.typeScriptTypes {
		t.Fatal("settings synchronization disabled command-line TypeScript analysis")
	}
}

func componentIndex(id string) int {
	for index, component := range componentWeights {
		if component.id == id {
			return index
		}
	}
	return -1
}

func columnIndex(key string) int {
	for index, column := range columnNames() {
		if column.key == key {
			return index
		}
	}
	return -1
}
