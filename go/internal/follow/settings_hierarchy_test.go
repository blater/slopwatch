package follow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSettingsHierarchyEveryLeafRestoresCursors(t *testing.T) {
	for groupIndex, group := range settingsItems {
		for leafIndex, leaf := range settingsGroups[group.key] {
			t.Run(group.key+"/"+leaf.key, func(t *testing.T) {
				model := Model{width: 100, height: 35, settings: true, settingsCursor: groupIndex, files: FilesState{Selected: "kept.go"}}
				handleKey(&model, tea.KeyMsg{Type: tea.KeyEnter})
				if model.settingsGroup != group.key || model.overlays.Len() != 2 {
					t.Fatalf("group=%s stack=%d", model.settingsGroup, model.overlays.Len())
				}
				model.settingsCursor = leafIndex
				handleKey(&model, tea.KeyMsg{Type: tea.KeyEnter})
				if model.overlays.Len() != 3 {
					t.Fatalf("leaf stack=%d", model.overlays.Len())
				}
				underlay := ansi.Strip(model.settingsUnderlay(strings.Repeat(".\n", 35)))
				if !strings.Contains(underlay, "SETTINGS") {
					t.Fatal("missing root underlay")
				}
				handleKey(&model, tea.KeyMsg{Type: tea.KeyEsc})
				if !model.settings || model.settingsGroup != group.key || model.settingsCursor != leafIndex {
					t.Fatal("leaf did not restore group cursor")
				}
				handleKey(&model, tea.KeyMsg{Type: tea.KeyEsc})
				if !model.settings || model.settingsGroup != "" || model.settingsCursor != groupIndex {
					t.Fatal("group did not restore root cursor")
				}
				handleKey(&model, tea.KeyMsg{Type: tea.KeyEsc})
				if model.settings || model.files.Selected != "kept.go" || model.overlays.Len() != 0 {
					t.Fatal("base selection not restored")
				}
			})
		}
	}
}

func TestConcurrencySavePreservesAgentDraftAndRevision(t *testing.T) {
	initial := settingsResolved()
	store := appconfig.NewMemory(initial)
	model := Model{configStore: store}
	load := model.openConfigSettings(configAgents)
	model.handleConfigResolved(load().(configResolvedMsg))
	model.configSettings.working.Profiles = []agent.Profile{{ID: "draft", Label: "Unsaved agent", Runtime: "codex-cli", Executable: "codex"}}
	model.configSettings.dirty = true
	model.configSettings.returnToFix = true
	model.configSettings.cursor = len(agentProviderChoices)
	model.openSelectedAgentProvider()
	if model.configSettings.kind != configConcurrency || model.runtime.configParent == nil {
		t.Fatal("concurrency missing")
	}
	model.configSettings.working.Concurrency.MaxAgents++
	model.configSettings.dirty = true
	save := model.closeConfigSettings()
	if save == nil {
		t.Fatal("no concurrency save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if model.runtime.configParent != nil || !model.configSettings.returnToFix || !model.configSettings.dirty || model.configSettings.working.Profiles[0].ID != "draft" {
		t.Fatal("parent edits or caller lost")
	}
	request, ok := model.configSettings.prepareSave(nil, store, model.configWorkspace, nil)
	if !ok {
		t.Fatalf("parent save unavailable: %s", model.configSettings.status)
	}
	result := request.command()().(configSavedMsg)
	if result.err != nil {
		t.Fatalf("parent save after child: %v", result.err)
	}
}

func TestAllFilteredRefreshClearsRowsWithoutErrorPopup(t *testing.T) {
	model := Model{files: FilesState{BaseDocument: report.Document{Files: []report.File{testFile("last.go", 1)}}, Document: report.Document{Files: []report.File{testFile("last.go", 1)}}, Rows: map[string]rowState{}}, weights: defaultWeights()}
	handleAnalysisResult(&model, analysisResult{full: true, err: native.ErrNoSources})
	if len(model.files.Document.Files) != 0 || model.runtimeError != "" {
		t.Fatalf("all-filtered result rows=%d error=%s", len(model.files.Document.Files), model.runtimeError)
	}
}

func settingsRefreshFixture(t *testing.T) *Model {
	t.Helper()
	root := t.TempDir()
	watchWrite(t, root, ".git", "gitdir: unused")
	watchWrite(t, root, "a.go", "package p")
	model, err := New(report.Document{Files: []report.File{testFile("a.go", 1)}}, &refreshAnalyzer{document: report.Document{Files: []report.File{testFile("a.go", 2)}}}, Options{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	return model
}

func TestStartupSelectionUsesFilteredProjection(t *testing.T) {
	root := t.TempDir()
	watchWrite(t, root, ".gitignore", "omit.go\n")
	watchWrite(t, root, "omit.go", "package p")
	watchWrite(t, root, "keep.go", "package p")
	model, err := New(report.Document{Files: []report.File{testFile("omit.go", 9), testFile("keep.go", 1)}}, &refreshAnalyzer{}, Options{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	if model.files.Selected != "keep.go" {
		t.Fatalf("selection=%q", model.files.Selected)
	}
}

func TestSettingsRetainWatcherResultsAndIncrementalQueue(t *testing.T) {
	model := settingsRefreshFixture(t)
	model.preferencesPath = filepath.Join(t.TempDir(), "preferences.toml")
	old := model.watcher
	model.analyzing = true
	model.queued = map[string]bool{"a.go": true}
	if command := model.toggleGitignore(); command != nil {
		t.Fatal("toggle scheduled analysis")
	}
	if model.watcher != old || len(model.files.Document.Files) != 1 || !model.analyzing || !model.queued["a.go"] {
		t.Fatal("settings changed active session")
	}
	if !strings.Contains(model.status, "restart") {
		t.Fatal("missing restart notice")
	}
}
