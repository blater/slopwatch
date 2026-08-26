package follow

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
)

func TestMainViewsSwitchAndRetainIndependentState(t *testing.T) {
	files := make([]report.File, 12)
	rows := make(map[string]rowState, len(files))
	for index := range files {
		path := strings.Repeat("long/", 20) + string(rune('a'+index)) + ".go"
		files[index] = testFile(path, float64(len(files)-index))
		rows[path] = rowState{}
	}
	selected := files[8].Path
	model := Model{
		width: 80, height: 10, document: report.Document{Files: files},
		rows: rows, visible: map[string]bool{},
		selected: selected, cursor: 8, offset: 3, pathOffset: 3,
		sortKey: "filename",
		agents: AgentsState{Selected: AgentRowID{JobID: "job-2"}, Offset: 2, HorizontalOffset: 7,
			Expanded: map[fix.JobID]bool{"job-2": true}},
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	result := updated.(*Model)
	if result.mainView != MainViewAgents {
		t.Fatalf("Tab selected main view %d, want Agents", result.mainView)
	}
	if result.agents.Selected != (AgentRowID{JobID: "job-2"}) || result.agents.Offset != 2 || result.agents.HorizontalOffset != 7 {
		t.Fatalf("Agents state changed on entry: %+v", result.agents)
	}
	if view := ansi.Strip(result.View()); !strings.Contains(view, "[AGENTS]") || !strings.Contains(view, "No fix jobs yet") {
		t.Fatalf("Agents shell was not rendered: %q", view)
	}

	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	result = updated.(*Model)
	if result.mainView != MainViewFiles {
		t.Fatalf("second Tab selected main view %d, want Files", result.mainView)
	}
	if result.selected != selected || result.cursor != 8 || result.offset != 3 || result.pathOffset != 3 || result.sortKey != "filename" || result.sortReverse {
		t.Fatalf("Files state was not restored: selected=%q cursor=%d offset=%d horizontal=%d sort=%q reverse=%t",
			result.selected, result.cursor, result.offset, result.pathOffset, result.sortKey, result.sortReverse)
	}
	if result.agents.Selected != (AgentRowID{JobID: "job-2"}) || !result.agents.Expanded["job-2"] {
		t.Fatalf("Agents state was not retained: %+v", result.agents)
	}
}

func TestDirectAgentsShortcutDoesNotResetAgentsState(t *testing.T) {
	model := Model{agents: AgentsState{Selected: AgentRowID{JobID: "job-7"}, ShowAll: true, Expanded: map[fix.JobID]bool{"job-7": true}}}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	result := updated.(*Model)
	if result.mainView != MainViewAgents || result.agents.Selected != (AgentRowID{JobID: "job-7"}) || !result.agents.ShowAll {
		t.Fatalf("A did not preserve and select Agents: view=%d state=%+v", result.mainView, result.agents)
	}
}

func TestTopOverlayOwnsKeysAndRecordsItsCaller(t *testing.T) {
	model := Model{width: 80, height: 20, visible: defaultColumnVisibility()}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	top, ok := result.overlays.Top()
	if !ok || top.Kind != OverlaySettings || top.Caller.MainView != MainViewFiles {
		t.Fatalf("Settings overlay stack = %+v, ok=%t", top, ok)
	}

	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	result = updated.(*Model)
	if result.mainView != MainViewFiles || !result.settings {
		t.Fatal("Tab escaped the top Settings overlay")
	}

	result.settingsCursor = settingsIndex("appearance")
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	top, ok = result.overlays.Top()
	if !ok || top.Kind != OverlayAppearance || top.Caller.Overlay != OverlaySettings {
		t.Fatalf("Appearance caller = %+v, ok=%t", top, ok)
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	result = updated.(*Model)
	top, ok = result.overlays.Top()
	if !ok || top.Kind != OverlaySettings {
		t.Fatalf("Escape did not restore Settings overlay: %+v, ok=%t", top, ok)
	}
}

func TestTypedOverlayOwnsKeysWithoutLegacyBooleanState(t *testing.T) {
	model := Model{mainView: MainViewFiles, width: 80, height: 20}
	model.overlays.Push(OverlayFixForm, OverlayCaller{MainView: MainViewFiles, Selected: "a.go"})

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	result := updated.(*Model)
	top, ok := result.overlays.Top()
	if result.mainView != MainViewFiles || !ok || top.Kind != OverlayFixForm || top.Caller.Selected != "a.go" {
		t.Fatalf("typed overlay did not retain key ownership: view=%d top=%+v ok=%t", result.mainView, top, ok)
	}

	removed, ok := result.overlays.Pop()
	if !ok || removed.Kind != OverlayFixForm || result.overlays.Len() != 0 {
		t.Fatalf("typed overlay pop = %+v, ok=%t, remaining=%d", removed, ok, result.overlays.Len())
	}
}

func TestBackgroundMessagesPreserveTopOverlayAndMainView(t *testing.T) {
	model := Model{width: 80, height: 20, mainView: MainViewAgents, settings: true}
	model.reconcileLegacyOverlayStack()
	updated, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if command != nil {
		t.Fatal("resize unexpectedly scheduled work")
	}
	result := updated.(*Model)
	top, ok := result.overlays.Top()
	if result.mainView != MainViewAgents || !ok || top.Kind != OverlaySettings {
		t.Fatalf("background resize changed routing: view=%d top=%+v ok=%t", result.mainView, top, ok)
	}
	if result.width != 100 || result.height != 30 {
		t.Fatalf("background resize was not applied: %dx%d", result.width, result.height)
	}
}

func TestResponsiveTiersAndFullScreenThresholds(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          ResponsiveTier
	}{
		{35, 6, ResponsiveResize}, {36, 5, ResponsiveResize},
		{36, 6, ResponsiveCompact}, {59, 30, ResponsiveCompact},
		{60, 6, ResponsiveMedium}, {95, 30, ResponsiveMedium},
		{96, 6, ResponsiveFull},
	} {
		if got := responsiveTier(test.width, test.height); got != test.want {
			t.Errorf("responsiveTier(%d, %d) = %d, want %d", test.width, test.height, got, test.want)
		}
	}
	for _, test := range []struct {
		width, height int
		want          bool
	}{{59, 16, true}, {60, 15, true}, {60, 16, false}} {
		if got := fullScreenSurface(test.width, test.height); got != test.want {
			t.Errorf("fullScreenSurface(%d, %d) = %t, want %t", test.width, test.height, got, test.want)
		}
	}
}

func TestResizeAndAgentsShellFitRequiredTerminalSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{24, 8}, {35, 5}} {
		model := Model{width: size.width, height: size.height}
		assertScreenSize(t, model.View(), size.width, size.height)
		if !strings.Contains(ansi.Strip(model.View()), "RESIZE") {
			t.Fatalf("%dx%d did not render resize safety", size.width, size.height)
		}
	}
	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}, {120, 30}} {
		model := Model{width: size.width, height: size.height, mainView: MainViewAgents}
		assertScreenSize(t, model.View(), size.width, size.height)
	}
}

func TestResizeScreenGatesInputForEveryHiddenOverlay(t *testing.T) {
	overlays := []OverlayKind{
		OverlayFind, OverlayInfo, OverlayHelp, OverlayDetail, OverlaySource, OverlayColumns, OverlaySort,
		OverlayWeights, OverlayAppearance, OverlaySettings, OverlayConfigSettings, OverlayFixForm,
		OverlayPromptEditor, OverlayPromptDetach, OverlayPromptDirty, OverlayJobMonitor, OverlayJobActions,
		OverlayJobLog, OverlayJobDiff, OverlayCandidateSource, OverlayConfirmation, OverlaySettingsDirty,
	}
	for _, kind := range overlays {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			service := &fakeFixService{draft: readyFixDraft("a.go")}
			model := fixTestModel(service, 35, 6)
			model.fixDialog = fixDialogState{hasDraft: true, draft: readyFixDraft("a.go"), cursor: fixFieldRun, detached: false}
			model.jobActions = jobActionsState{jobID: "job", revision: 1, actions: []fix.JobAction{fix.ActionRetry}}
			model.cancelConfirmation = cancelConfirmation{jobID: "job", revision: 1, action: fix.ActionDiscard, allowed: true}
			model.configSettings = configSettingsState{open: true, dirty: true, dirtyCursor: 1}
			model.overlays.Push(kind, OverlayCaller{MainView: MainViewFiles, Selected: "a.go"})
			beforeLen := model.overlays.Len()
			beforeCursor := model.fixDialog.cursor
			beforePending := model.cancelConfirmation.pending

			updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			result := updated.(*Model)
			top, ok := result.overlays.Top()
			if command != nil || !ok || top.Kind != kind || result.overlays.Len() != beforeLen || result.fixDialog.cursor != beforeCursor ||
				result.cancelConfirmation.pending != beforePending || service.executed.JobID != "" {
				t.Fatalf("hidden overlay %d consumed Enter: top=%+v len=%d command=%v executed=%+v", kind, top, result.overlays.Len(), command, service.executed)
			}
			if view := ansi.Strip(result.View()); !strings.Contains(view, "RESIZE TERMINAL") {
				t.Fatalf("hidden overlay %d escaped resize surface: %q", kind, view)
			}
		})
	}
}

func TestResizeScreenAllowsOnlyVisibleShutdownConfirmation(t *testing.T) {
	service := &fakeFixService{}
	visible := fixTestModel(service, 35, 6)
	visible.shutdown = shutdownState{active: 1}
	visible.overlays.Push(OverlayShutdown, OverlayCaller{MainView: MainViewAgents})
	if view := ansi.Strip(visible.View()); !strings.Contains(view, "ACTIVE FIX JOBS") || !strings.Contains(view, "Enter cancel all + quit") {
		t.Fatalf("visible compact shutdown was not rendered: %q", view)
	}
	_, command := visible.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !visible.shutdown.pending {
		t.Fatal("visible shutdown confirmation did not accept Enter")
	}

	hidden := fixTestModel(service, 20, 1)
	hidden.shutdown = shutdownState{active: 1}
	hidden.overlays.Push(OverlayShutdown, OverlayCaller{MainView: MainViewAgents})
	before := hidden.overlays.Len()
	if view := ansi.Strip(hidden.View()); !strings.Contains(view, "RESIZE TERMINAL") || strings.Contains(view, "ACTIVE FIX JOBS") {
		t.Fatalf("unsafe shutdown surface was visible below 24x2: %q", view)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'q'}}} {
		_, command = hidden.handleKey(key)
		if command != nil || hidden.shutdown.pending || hidden.overlays.Len() != before {
			t.Fatalf("hidden shutdown consumed %q: pending=%t overlays=%d command=%v", key.String(), hidden.shutdown.pending, hidden.overlays.Len(), command)
		}
	}
}

func assertScreenSize(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("screen height = %d, want %d: %q", len(lines), height, ansi.Strip(view))
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("line %d width = %d, want %d: %q", index, got, width, ansi.Strip(line))
		}
	}
}
