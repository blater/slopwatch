package follow

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func TestMarkModeTogglesFilesAndClearAction(t *testing.T) {
	ConfigureTerminalColours()
	files := []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model := Model{
		files: FilesState{
			Selected: "a.go",
			Document: report.Document{Files: files},
			Rows:     map[string]rowState{},
			Visible:  defaultColumnVisibility(),
			Marked:   map[string]bool{},
		},
		width:  80,
		height: 8,
	}
	result := enterMarkingMode(t, model, files)
	markSelectedRows(t, result)
	finishMarkingMode(t, result, files)
	clearMarks(t, result)
}

func enterMarkingMode(t *testing.T, model Model, files []report.File) *Model {
	t.Helper()
	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	result := updated.(*Model)
	if strings.Contains(ansi.Strip(footer(*result)), "clear") {
		t.Fatal("clear is visible before any file is marked")
	}
	if !result.files.Marking || !strings.Contains(ansi.Strip(footer(*result)), "done") {
		t.Fatalf("mark mode was not visible: marking=%t footer=%q", result.files.Marking, ansi.Strip(footer(*result)))
	}
	if got := ansi.Strip(renderRow(*result, files[0], true)); !strings.HasPrefix(got, "( ) ") {
		t.Fatalf("mark control is not at the left of the row: %q", got)
	}
	return result
}

func markSelectedRows(t *testing.T, result *Model) {
	t.Helper()
	handleKey(result, tea.KeyMsg{Type: tea.KeySpace})
	if !result.files.Marked["a.go"] {
		t.Fatalf("Space did not mark the current row: %v", result.files.Marked)
	}
	if !strings.Contains(ansi.Strip(footer(*result)), "clear") {
		t.Fatal("clear is hidden after marking a file")
	}
	handleKey(result, tea.KeyMsg{Type: tea.KeySpace})
	if strings.Contains(ansi.Strip(footer(*result)), "clear") {
		t.Fatal("clear remains visible after unmarking the last file")
	}
	handleKey(result, tea.KeyMsg{Type: tea.KeyShiftDown})
	if !result.files.Marked["a.go"] || !result.files.Marked["b.go"] || result.files.Cursor != 1 {
		t.Fatalf("Shift-Down marks = %v cursor=%d", result.files.Marked, result.files.Cursor)
	}
}

func finishMarkingMode(t *testing.T, result *Model, files []report.File) {
	t.Helper()
	handleKey(result, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if result.files.Marking || !strings.HasPrefix(ansi.Strip(renderRow(*result, files[0], false)), "● ") {
		t.Fatalf("completed marks are not compact: marking=%t row=%q", result.files.Marking, ansi.Strip(renderRow(*result, files[0], false)))
	}
	if got := rowBackground(rowState{}, false, true, 0); got != style.SurfaceMarked {
		t.Fatalf("marked row background = %q, want %q", got, style.SurfaceMarked)
	}
	if got := rowBackground(rowState{}, true, true, 0); got != style.SurfaceSelected {
		t.Fatalf("cursor must remain visible over a mark: got %q", got)
	}
}

func clearMarks(t *testing.T, result *Model) {
	t.Helper()
	handleKey(result, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if len(result.files.Marked) != 0 || !strings.Contains(ansi.Strip(footer(*result)), "mark") || strings.Contains(ansi.Strip(footer(*result)), "clear") {
		t.Fatalf("clear did not restore the unmarked view: marks=%v footer=%q", result.files.Marked, ansi.Strip(footer(*result)))
	}
}

func TestShiftArrowTogglesBothRowsInEitherDirection(t *testing.T) {
	files := []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	for _, test := range []struct {
		name  string
		key   tea.KeyType
		start int
		want  map[string]bool
	}{
		{name: "down", key: tea.KeyShiftDown, start: 0, want: map[string]bool{"a.go": true, "b.go": true}},
		{name: "up", key: tea.KeyShiftUp, start: 2, want: map[string]bool{"b.go": true, "c.go": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &Model{
				files: FilesState{
					Cursor:   test.start,
					Marking:  true,
					Document: report.Document{Files: files},
					Rows:     map[string]rowState{},
					Visible:  defaultColumnVisibility(),
					Marked:   map[string]bool{},
				}}
			handleKey(model, tea.KeyMsg{Type: test.key})
			if len(model.files.Marked) != 2 {
				t.Fatalf("marks = %v", model.files.Marked)
			}
			for path := range test.want {
				if !model.files.Marked[path] {
					t.Fatalf("Shift-%s omitted %s: %v", test.name, path, model.files.Marked)
				}
			}
		})
	}
}

func TestHeldShiftMarksEachCrossedRowOnce(t *testing.T) {
	files := []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model := &Model{
		files: FilesState{
			Marking:  true,
			Document: report.Document{Files: files},
			Rows:     map[string]rowState{},
			Visible:  defaultColumnVisibility(),
			Marked:   map[string]bool{},
		}}
	handleKey(model, tea.KeyMsg{Type: tea.KeyShiftDown})
	handleKey(model, tea.KeyMsg{Type: tea.KeyShiftDown})
	if model.files.Cursor != 2 || len(model.files.Marked) != 3 {
		t.Fatalf("held Shift-Down marks = %v cursor=%d", model.files.Marked, model.files.Cursor)
	}
	for _, path := range []string{"a.go", "b.go", "c.go"} {
		if !model.files.Marked[path] {
			t.Fatalf("held Shift-Down omitted %s: %v", path, model.files.Marked)
		}
	}
}

func TestFixUsesMarkedFilesAndFallsBackToCurrentFile(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	model.files.Document.Files = []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model.files.Marked = map[string]bool{"a.go": true, "b.go": true}
	model.files.Selected = "c.go"

	command := model.openFixForSelected()
	if command == nil {
		t.Fatal("marked files did not open Fix")
	}
	command()
	if got, want := service.prepareRequest.Targets, []fix.RepoPath{"a.go", "b.go"}; !repoPathsEqual(got, want) {
		t.Fatalf("Fix targets = %v, want %v", got, want)
	}
	if !strings.Contains(model.fixDialog.statusText, "2 files") {
		t.Fatalf("multi-file preparation status = %q", model.fixDialog.statusText)
	}

	model.overlays.Pop()
	model.clearMarkedFiles()
	command = model.openFixForSelected()
	command()
	if got, want := service.prepareRequest.Targets, []fix.RepoPath{"c.go"}; !repoPathsEqual(got, want) {
		t.Fatalf("unmarked Fix targets = %v, want %v", got, want)
	}
}

func repoPathsEqual(left, right []fix.RepoPath) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
