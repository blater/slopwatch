package follow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestProjectExclusionsEditorSavesForRestart(t *testing.T) {
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
	model.analyzer.(*refreshAnalyzer).document = report.Document{}
	_, command := dispatchKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q.go\na.go")})
	if model.runtime.filesExclusions.Value() != "q.go\na.go" {
		t.Fatalf("raw text lost: %q", model.runtime.filesExclusions.Value())
	}
	if !model.runtime.filesEditing || len(model.files.Document.Files) != 1 {
		t.Fatal("edit changed current results")
	}
	_ = command
	dispatchKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	document, err := preferences.LoadProject(model.options.Workspace)
	if err != nil || model.runtime.filesEditing || document.Files.Exclude != "q.go\na.go" {
		t.Fatalf("close did not retain saved edits: %+v %v", document, err)
	}
	if _, _, ok := model.watcher.eligible(filepath.Join(model.options.Workspace, "a.go")); !ok {
		t.Fatal("saved exclusion changed active watcher")
	}
	model.openFilesExclusions()
	model.analyzer.(*refreshAnalyzer).document = report.Document{Files: []report.File{testFile("a.go", 2)}}
	// Ctrl+U removes the current line's exclusion without a save action.
	_, command = dispatchKey(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	_ = command
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
	path := filepath.Join(model.options.Workspace, ".slopwatch.toml")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	model.handleFilesExclusionsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	if !model.runtime.filesEditing || model.runtimeError == "" || model.runtime.filesExclusions.Value() != text {
		t.Fatal("save failure discarded editor")
	}
	dispatchKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.runtimeError != "" || !model.runtime.filesEditing || model.runtime.filesExclusions.Value() != text {
		t.Fatal("dismissing save error did not restore the unchanged editor")
	}
}

func TestProjectExclusionsUnchangedMessagesDoNotSave(t *testing.T) {
	model := settingsRefreshFixture(t)
	openSetting(model, "files")
	model.openFilesExclusions()
	previous := model.watcher
	for _, message := range []tea.Msg{tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyCtrlS}, cursor.BlinkMsg{}} {
		handleMessage(model, message)
	}
	if _, err := os.Stat(filepath.Join(model.options.Workspace, ".slopwatch.toml")); !os.IsNotExist(err) {
		t.Fatalf("unchanged text created a file: %v", err)
	}
	if model.watcher != previous {
		t.Fatal("unchanged text refreshed watcher")
	}
}

func TestProjectExclusionsAsyncPasteAutomaticallySaves(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("isolated pbpaste fixture requires macOS")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pbpaste"), []byte("#!/bin/sh\nprintf 'paste.go\\nother.go'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	model := settingsRefreshFixture(t)
	openSetting(model, "files")
	model.openFilesExclusions()
	_, command := handleMessage(model, textarea.Paste())
	document, err := preferences.LoadProject(model.options.Workspace)
	if err != nil || document.Files.Exclude != "paste.go\nother.go" || !model.runtime.filesEditing {
		t.Fatalf("async paste was not saved: %+v %v", document, err)
	}
	_ = command
}

func TestMalformedProjectRejectsNewWatcher(t *testing.T) {
	model := settingsRefreshFixture(t)
	watchWrite(t, model.options.Workspace, ".slopwatch.toml", "[files]\nexclude = 7")
	if watcher, err := newSourceWatcher(model.options.Workspace, nil, true, false, nil); err == nil {
		watcher.close()
		t.Fatal("watcher swallowed malformed preferences")
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
