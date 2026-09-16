package follow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
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
	savePreferenceFixture(t, path)

	analyzer := &preferenceAnalyzer{}
	model, err := New(report.Document{}, analyzer, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	assertPreferenceFixtureLoaded(t, model, analyzer)

	overridden, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path, TrendWindow: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer overridden.Close()
	assertTrendOverride(t, overridden, path)
}

func savePreferenceFixture(t *testing.T, path string) {
	t.Helper()
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
}

func assertPreferenceFixtureLoaded(t *testing.T, model *Model, analyzer *preferenceAnalyzer) {
	t.Helper()
	assertFixtureTheme(t, model)
	assertFixtureTable(t, model)
	assertFixtureTuning(t, model)
	assertFixtureScoring(t, model, analyzer)
}

func assertFixtureTheme(t *testing.T, model *Model) {
	t.Helper()
	if model.theme != style.ThemeLight || string(style.SurfaceScreen) != "#f7fafc" {
		t.Fatalf("theme was not loaded: model=%q surface=%q", model.theme, style.SurfaceScreen)
	}
}

func assertFixtureTable(t *testing.T, model *Model) {
	t.Helper()
	if len(model.files.Visible) != 1 || !model.files.Visible["cog"] || model.files.SortKey != "filename" || model.files.SortReverse {
		t.Fatalf("table preferences were not loaded: visible=%v sort=%s reverse=%t", model.files.Visible, model.files.SortKey, model.files.SortReverse)
	}
}

func assertFixtureTuning(t *testing.T, model *Model) {
	t.Helper()
	if model.options.TrendWindow != 42*time.Minute || model.weightStep != 0.25 || model.maximumWeight != 12 {
		t.Fatalf("tuning preferences were not loaded: trend=%s step=%v max=%v", model.options.TrendWindow, model.weightStep, model.maximumWeight)
	}
}

func assertFixtureScoring(t *testing.T, model *Model, analyzer *preferenceAnalyzer) {
	t.Helper()
	if model.weights["cognitive_complexity"] != 3 || !analyzer.typeScriptTypes {
		t.Fatalf("scoring preferences were not loaded: weight=%v types=%t", model.weights["cognitive_complexity"], analyzer.typeScriptTypes)
	}
}

func assertTrendOverride(t *testing.T, overridden *Model, path string) {
	t.Helper()
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
	model.adjustWeight(positiveOrDefault(model.weightStep, defaultWeightStep))
	toggleWeight(model)
	model.columnCursor = columnIndex("cog")
	handleColumnKey(model, " ")
	model.files.SortCursor = len(sortFields()) - 1
	activateHighlightedSort(model, false, true)
	model.Close()

	restarted, err := New(report.Document{}, &preferenceAnalyzer{}, Options{
		Workspace: workspace, Targets: []string{"."}, PreferencesPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	assertRestartedTheme(t, restarted)
	assertRestartedWeight(t, restarted)
	assertRestartedColumns(t, restarted)
	assertRestartedSort(t, restarted)
}

func assertRestartedTheme(t *testing.T, restarted *Model) {
	t.Helper()
	if restarted.theme != style.ThemeLight {
		t.Fatalf("restarted theme = %q", restarted.theme)
	}
}

func assertRestartedWeight(t *testing.T, restarted *Model) {
	t.Helper()
	if restarted.weights["cognitive_complexity"] != 10.5 {
		t.Fatalf("restarted weight = %v", restarted.weights["cognitive_complexity"])
	}
	if isWeightEnabled(*restarted, "cognitive_complexity") {
		t.Fatal("restarted model restored a disabled component")
	}
}

func assertRestartedColumns(t *testing.T, restarted *Model) {
	t.Helper()
	if restarted.files.Visible["cog"] {
		t.Fatal("restarted model restored a hidden column")
	}
}

func assertRestartedSort(t *testing.T, restarted *Model) {
	t.Helper()
	if restarted.files.SortKey != "filename" || restarted.files.SortReverse {
		t.Fatalf("restarted sort = %s reverse=%t", restarted.files.SortKey, restarted.files.SortReverse)
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
