package follow

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/report"
	"github.com/blater/slopmochi/internal/style"
)

func TestMarkModeTogglesFilesAndKeepsPermanentActions(t *testing.T) {
	ConfigureTerminalColours()
	files := []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model := Model{
		width: 80, height: 8, selected: "a.go", document: report.Document{Files: files},
		rows: map[string]rowState{}, visible: defaultColumnVisibility(), marked: map[string]bool{},
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	result := updated.(*Model)
	if !result.marking || !strings.Contains(ansi.Strip(result.footer()), "done") {
		t.Fatalf("mark mode was not visible: marking=%t footer=%q", result.marking, ansi.Strip(result.footer()))
	}
	if got := ansi.Strip(result.renderRow(files[0], true)); !strings.HasPrefix(got, "( ) ") {
		t.Fatalf("mark control is not at the left of the row: %q", got)
	}

	result.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	if !result.marked["a.go"] {
		t.Fatalf("Space did not mark the current row: %v", result.marked)
	}
	result.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	result.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	if !result.marked["a.go"] || !result.marked["b.go"] || result.cursor != 1 {
		t.Fatalf("Shift-Down marks = %v cursor=%d", result.marked, result.cursor)
	}

	result.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if result.marking || !strings.HasPrefix(ansi.Strip(result.renderRow(files[0], false)), "● ") {
		t.Fatalf("completed marks are not compact: marking=%t row=%q", result.marking, ansi.Strip(result.renderRow(files[0], false)))
	}
	if got := rowBackground(rowState{}, false, true, 0); got != style.SurfaceMarked {
		t.Fatalf("marked row background = %q, want %q", got, style.SurfaceMarked)
	}
	if got := rowBackground(rowState{}, true, true, 0); got != style.SurfaceSelected {
		t.Fatalf("cursor must remain visible over a mark: got %q", got)
	}

	result.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	if len(result.marked) != 0 || !strings.Contains(ansi.Strip(result.footer()), "mark") || !strings.Contains(ansi.Strip(result.footer()), "clear") {
		t.Fatalf("clear did not restore the unmarked view: marks=%v footer=%q", result.marked, ansi.Strip(result.footer()))
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
			model := &Model{cursor: test.start, marking: true, document: report.Document{Files: files}, rows: map[string]rowState{}, visible: defaultColumnVisibility(), marked: map[string]bool{}}
			model.handleKey(tea.KeyMsg{Type: test.key})
			if len(model.marked) != 2 {
				t.Fatalf("marks = %v", model.marked)
			}
			for path := range test.want {
				if !model.marked[path] {
					t.Fatalf("Shift-%s omitted %s: %v", test.name, path, model.marked)
				}
			}
		})
	}
}

func TestHeldShiftMarksEachCrossedRowOnce(t *testing.T) {
	files := []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model := &Model{marking: true, document: report.Document{Files: files}, rows: map[string]rowState{}, visible: defaultColumnVisibility(), marked: map[string]bool{}}
	model.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	model.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	if model.cursor != 2 || len(model.marked) != 3 {
		t.Fatalf("held Shift-Down marks = %v cursor=%d", model.marked, model.cursor)
	}
	for _, path := range []string{"a.go", "b.go", "c.go"} {
		if !model.marked[path] {
			t.Fatalf("held Shift-Down omitted %s: %v", path, model.marked)
		}
	}
}

func TestFixUsesMarkedFilesAndFallsBackToCurrentFile(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	model.document.Files = []report.File{testFile("a.go", 30), testFile("b.go", 20), testFile("c.go", 10)}
	model.marked = map[string]bool{"a.go": true, "b.go": true}
	model.selected = "c.go"

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
