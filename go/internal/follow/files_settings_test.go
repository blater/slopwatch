package follow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestProjectExclusionsEditorSaveCancelAndRefresh(t *testing.T) {
	model := settingsRefreshFixture(t)
	model.preferencesPath = filepath.Join(t.TempDir(), "preferences.toml")
	if err := preferences.Save(model.preferencesPath, model.preferences); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(model.preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	openSetting(model, "files")
	handleSettingsKey(model, "down")
	handleSettingsKey(model, "enter")
	// Ordinary shortcut letters and pasted multiline text belong to the editor.
	dispatchKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q.go\na.go")})
	if model.runtime.filesExclusions.Value() != "q.go\na.go" {
		t.Fatalf("raw text lost: %q", model.runtime.filesExclusions.Value())
	}
	dispatchKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.runtime.filesEditing {
		t.Fatal("cancel left editor open")
	}
	if _, err := os.Stat(filepath.Join(model.options.Workspace, ".slopwatch.toml")); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote preferences: %v", err)
	}
	model.openFilesExclusions()
	model.runtime.filesExclusions.SetValue("a.go\n")
	model.analyzer.(*refreshAnalyzer).document = report.Document{}
	_, command := dispatchKey(model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if command == nil || model.runtime.filesEditing || len(model.files.Document.Files) != 0 {
		t.Fatal("save did not close, prune, and schedule refresh")
	}
	executeReplacementAnalysis(t, model, model.handleWatcherReconfigured(command().(watcherReconfigured)))
	if _, _, ok := model.watcher.eligible(filepath.Join(model.options.Workspace, "a.go")); ok {
		t.Fatal("saved exclusion remains eligible for watching")
	}
	model.openFilesExclusions()
	model.runtime.filesExclusions.SetValue("")
	model.analyzer.(*refreshAnalyzer).document = report.Document{Files: []report.File{testFile("a.go", 2)}}
	_, command = dispatchKey(model, tea.KeyMsg{Type: tea.KeyCtrlS})
	executeReplacementAnalysis(t, model, model.handleWatcherReconfigured(command().(watcherReconfigured)))
	if len(model.files.Document.Files) != 1 {
		t.Fatal("removed exclusion did not return file to inventory")
	}
	if _, _, ok := model.watcher.eligible(filepath.Join(model.options.Workspace, "a.go")); !ok {
		t.Fatal("removed exclusion did not restore watcher eligibility")
	}
	after, err := os.ReadFile(model.preferencesPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("local editor changed global preferences: %v", err)
	}
}

func TestProjectExclusionsEditorFailureAndLongText(t *testing.T) {
	model := settingsRefreshFixture(t)
	openSetting(model, "files")
	model.openFilesExclusions()
	text := strings.Repeat("generated.go\n", 150)
	model.runtime.filesExclusions.SetValue(text)
	if model.runtime.filesExclusions.Value() != text {
		t.Fatal("editor truncated exclusions")
	}
	path := filepath.Join(model.options.Workspace, ".slopwatch.toml")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, command := model.handleFilesExclusionsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if command != nil || !model.runtime.filesEditing || model.runtimeError == "" || model.runtime.filesExclusions.Value() != text {
		t.Fatal("save failure discarded editor")
	}
	dispatchKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.runtimeError != "" || !model.runtime.filesEditing || model.runtime.filesExclusions.Value() != text {
		t.Fatal("dismissing save error did not restore the unchanged editor")
	}
}

func TestMalformedProjectDoesNotPersistGitignoreToggle(t *testing.T) {
	model := settingsRefreshFixture(t)
	model.preferencesPath = filepath.Join(t.TempDir(), "preferences.toml")
	if err := preferences.Save(model.preferencesPath, model.preferences); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(model.preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	watchWrite(t, model.options.Workspace, ".slopwatch.toml", "[files]\nexclude = false")
	disabled, watcher := model.options.DisableGitignore, model.watcher
	model.toggleGitignore()
	after, err := os.ReadFile(model.preferencesPath)
	if err != nil || string(before) != string(after) || model.options.DisableGitignore != disabled || model.watcher != watcher || model.runtimeError == "" {
		t.Fatal("failed toggle changed displayed, stored, or active policy")
	}
}

func TestMalformedProjectRejectsWatcherAndKeepsRefreshState(t *testing.T) {
	model := settingsRefreshFixture(t)
	watchWrite(t, model.options.Workspace, ".slopwatch.toml", "[files]\nexclude = 7")
	if watcher, err := newSourceWatcher(model.options.Workspace, nil, true, false, nil); err == nil {
		watcher.close()
		t.Fatal("watcher swallowed malformed project preferences")
	}
	previous, generation := model.watcher, model.runtime.watchGeneration
	model.runtime.watchNeedsWait = true
	command := model.refreshIgnorePolicy()
	if command == nil || model.watcher != previous || model.runtime.watchGeneration != generation || model.runtime.watchReconfigurePending || model.runtime.watchNeedsWait || model.runtimeError == "" {
		t.Fatal("load failure disrupted existing watcher state")
	}
}

func TestProjectExclusionsEditorFitsCompactTerminal(t *testing.T) {
	model := settingsRefreshFixture(t)
	openSetting(model, "files")
	model.openFilesExclusions()
	handleWindowSize(model, tea.WindowSizeMsg{Width: 36, Height: 6})
	view := filesSettingsView(*model)
	if lipgloss.Width(view) > 36 || lipgloss.Height(view) > 6 {
		t.Fatalf("editor is %dx%d in a 36x6 terminal", lipgloss.Width(view), lipgloss.Height(view))
	}
}
