package follow

import (
	"errors"
	"fmt"
	"os"
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
	if model.configSettings.kind != configConcurrency || model.configParent == nil {
		t.Fatal("concurrency missing")
	}
	model.configSettings.working.Concurrency.MaxAgents++
	model.configSettings.dirty = true
	save := model.closeConfigSettings()
	if save == nil {
		t.Fatal("no concurrency save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if model.configParent != nil || !model.configSettings.returnToFix || !model.configSettings.dirty || model.configSettings.working.Profiles[0].ID != "draft" {
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

func TestToggleDropsSupersededAnalysisAndWatcher(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{"keep.go": "package p", "omit.go": "package p", ".gitignore": "omit.go\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	analyzer := &refreshAnalyzer{}
	model, err := New(report.Document{Files: []report.File{testFile("keep.go", 1), testFile("omit.go", 2)}}, analyzer, Options{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	model.options.DisableGitignore = true
	old := model.watcher
	model.analyzing = true
	command := model.toggleGitignore()
	if len(model.files.Document.Files) != 1 || !model.discardAnalysis || !model.pendingFullAnalysis {
		t.Fatal("policy change did not remove ignored rows or supersede old result")
	}
	message := command().(watcherReconfigured)
	model.handleWatcherReconfigured(message)
	if model.watcher == old {
		t.Fatalf("watch registrations not replaced: %v / %s", message.err, model.runtimeError)
	}
	handleAnalysisResult(model, analysisResult{full: true, document: report.Document{Files: []report.File{testFile("omit.go", 2)}}})
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "keep.go" {
		t.Fatal("old analysis restored ignored row")
	}
	if _, cmd := handleSourceChange(model, sourceChange{watcher: old, Err: os.ErrClosed}); cmd != nil || model.runtimeError != "" {
		t.Fatal("retired watcher event was accepted")
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

func executeReplacementAnalysis(t *testing.T, model *Model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("replacement did not schedule analysis")
	}
	batch, ok := command().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("replacement commands=%T %v", batch, batch)
	}
	result, ok := batch[1]().(analysisResult)
	if !ok {
		t.Fatal("missing real analysis result")
	}
	handleAnalysisResult(model, result)
	if model.analyzing || model.pendingFullAnalysis || model.discardAnalysis {
		t.Fatal("replacement failed to settle")
	}
}

func TestToggleBeforeInitialWatcherReadyStartsFreshAnalysis(t *testing.T) {
	model := settingsRefreshFixture(t)
	model.StartInitialAnalysis()
	old := model.watcher
	command := model.toggleGitignore()
	message := command().(watcherReconfigured)
	next := model.handleWatcherReconfigured(message)
	if model.startupWatcherPending || model.discardAnalysis {
		t.Fatal("unstarted initial analysis treated as running job")
	}
	if _, cmd := handleWatcherReady(model, watcherReady{watcher: old}); cmd != nil {
		t.Fatal("retired startup watcher launched analysis")
	}
	executeReplacementAnalysis(t, model, next)
}

func TestToggleDuringRetryDoesNotDiscardFreshRetryResult(t *testing.T) {
	model := settingsRefreshFixture(t)
	model.analyzing = true
	model.analysisRetryPending = true
	command := model.toggleGitignore()
	if model.discardAnalysis || model.analysisRetryPending {
		t.Fatal("retry delay treated as in-flight work")
	}
	next := model.handleWatcherReconfigured(command().(watcherReconfigured))
	if _, cmd := handleAnalysisRetry(model); cmd != nil {
		t.Fatal("old retry tick launched duplicate job")
	}
	executeReplacementAnalysis(t, model, next)
	if model.files.Document.Files[0].Score != 2 {
		t.Fatal("fresh replacement result discarded")
	}
}

func TestWatcherReplacementFailureResumesOnlyConsumedWaitAndRetries(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		t.Run(fmt.Sprint(consumed), func(t *testing.T) {
			model := settingsRefreshFixture(t)
			model.watchGeneration = 1
			model.watchReconfigurePending = true
			model.watchNeedsWait = consumed
			model.pendingFullAnalysis = true
			model.analyzing = true
			command := model.handleWatcherReconfigured(watcherReconfigured{generation: 1, err: errors.New("injected registration failure")})
			if command == nil {
				t.Fatal("no replacement recovery")
			}
			if consumed {
				batch, ok := command().(tea.BatchMsg)
				if !ok || len(batch) != 2 {
					t.Fatal("consumed wait not resumed exactly once")
				}
			} else {
				if _, ok := command().(watcherReconfigureRetry); !ok {
					t.Fatal("toggle failure scheduled duplicate watcher wait")
				}
			}
			if model.watchNeedsWait || model.watchRetryCount != 1 {
				t.Fatal("retry state not bounded")
			}
			retry := model.refreshIgnorePolicy()
			replacement := retry().(watcherReconfigured)
			if replacement.err != nil {
				t.Fatal(replacement.err)
			}
			model.handleWatcherReconfigured(replacement)
			if model.runtimeError != "" {
				t.Fatal("transient replacement failure opened ERROR popup")
			}
			if model.watchReconfigurePending || model.watchRetryCount != 0 {
				t.Fatal("replacement did not recover")
			}
		})
	}
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
