package follow

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func TestMainTableGAndShiftGJumpWithoutFooterHint(t *testing.T) {
	files := make([]report.File, 8)
	for index := range files {
		files[index] = report.File{Path: string(rune('a'+index)) + ".go", Rank: index + 1}
	}
	model := Model{
		files: FilesState{
			Document: report.Document{Files: files},
			SortKey:  "filename",
			Visible:  defaultColumnVisibility(),
		},
		width:  80,
		height: 6,
	}

	handleKey(&model, runeKey('G'))
	if model.files.Cursor != len(files)-1 || model.files.Selected != "h.go" || model.files.Offset == 0 {
		t.Fatalf("G selected cursor=%d path=%q offset=%d", model.files.Cursor, model.files.Selected, model.files.Offset)
	}
	handleKey(&model, runeKey('g'))
	if model.files.Cursor != 0 || model.files.Selected != "a.go" || model.files.Offset != 0 {
		t.Fatalf("g selected cursor=%d path=%q offset=%d", model.files.Cursor, model.files.Selected, model.files.Offset)
	}
	if footer := ansi.Strip(footer(model)); strings.Contains(strings.ToLower(footer), "jump") || strings.Contains(footer, "g/G") {
		t.Fatalf("footer advertises the convenience jump: %q", footer)
	}
}

func TestHelpTopicsOpenTheirReferencePages(t *testing.T) {
	model := Model{width: 100, height: 40}
	handleKey(&model, runeKey('h'))
	chooser := ansi.Strip(helpView(model))
	for _, topic := range []string{"Command-line options", "Main-screen controls", "Scoring system"} {
		if !strings.Contains(chooser, topic) {
			t.Errorf("help chooser does not contain %q", topic)
		}
	}

	handleHelpKey(&model, "enter")
	commandLine := ansi.Strip(helpView(model))
	for _, option := range []string{"--format text|json", "--pass-score SCORE", "--typescript-types"} {
		if !strings.Contains(commandLine, option) {
			t.Errorf("command-line help does not contain %q", option)
		}
	}

	handleHelpKey(&model, "esc")
	model.helpCursor = 1
	handleHelpKey(&model, "enter")
	controls := strings.Join(topicLines(helpMainScreen, 100, 0), "\n")
	for _, description := range []string{"G or End selects the final file", "g or Home selects the first file", "s opens alphabetically ordered settings"} {
		if !strings.Contains(controls, description) {
			t.Errorf("main-screen help does not contain %q", description)
		}
	}

	handleHelpKey(&model, "esc")
	model.helpCursor = 2
	handleHelpKey(&model, "enter")
	if scoring := ansi.Strip(helpView(model)); !strings.Contains(scoring, "SCORE") || !strings.Contains(scoring, "COG") {
		t.Fatalf("scoring help does not retain metric information: %q", scoring)
	}
}

func TestMainScreenHelpIsAlphabeticalByAction(t *testing.T) {
	labels := make([]string, 0, len(mainScreenHelp))
	for _, entry := range mainScreenHelp {
		labels = append(labels, entry.label)
	}
	if !slices.IsSorted(labels) {
		t.Fatalf("main-screen help is not alphabetical: %v", labels)
	}
}

func TestSettingsAreAlphabeticalAndLightThemeAppliesEverywhere(t *testing.T) {
	ConfigureTerminalColours()
	t.Cleanup(ConfigureTerminalColours)
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{{Path: "main.go", Rank: 1}}},
			Visible:  defaultColumnVisibility(),
			Rows:     map[string]rowState{},
		},
		width: 80, height: 20, theme: style.ThemeDark, settings: true,
	}
	settings := ansi.Strip(settingsView(model))
	appearanceAt, columnsAt, weightsAt := strings.Index(settings, "Appearance"), strings.Index(settings, "Columns"), strings.Index(settings, "Weights")
	if appearanceAt < 0 || appearanceAt >= columnsAt || columnsAt >= weightsAt {
		t.Fatalf("settings are not alphabetical: %q", settings)
	}

	model.settingsCursor = settingsIndex("appearance")
	handleSettingsKey(&model, "enter")
	if !model.appearance || model.settings {
		t.Fatal("Appearance did not open from Settings")
	}
	handleAppearanceKey(&model, "down")
	handleAppearanceKey(&model, "enter")
	if model.theme != style.ThemeLight || string(style.SurfaceScreen) != "#f7fafc" {
		t.Fatalf("light theme was not applied: theme=%q screen=%q", model.theme, style.SurfaceScreen)
	}

	views := map[string]string{
		"main table":       tableView(model),
		"appearance popup": appearanceView(model),
		"columns popup":    columnsView(model),
		"settings popup":   settingsView(model),
		"sort popup":       sortView(model),
		"weights popup":    weightsView(model),
	}
	model.help = true
	views["help popup"] = helpView(model)
	model.help = false
	model.infoKey = "score"
	views["info popup"] = infoView(model)
	model.detail = true
	views["detail popup"] = detailView(model)
	for name, view := range views {
		if !strings.Contains(view, "48;2;") {
			t.Errorf("%s does not render a themed true-colour background", name)
		}
		if strings.Contains(view, "48;2;7;16;25") || strings.Contains(view, "48;2;13;29;41") {
			t.Errorf("%s still contains a dark application surface", name)
		}
	}

	model.settings = false
	model.appearance = true
	model.appearanceCursor = 0
	handleAppearanceKey(&model, "esc")
	handleSettingsKey(&model, "enter")
	if model.appearanceCursor != 1 {
		t.Fatalf("Appearance did not remember the selected theme: cursor=%d", model.appearanceCursor)
	}
}

func runeKey(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}
