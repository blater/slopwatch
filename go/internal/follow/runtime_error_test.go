package follow

import (
	"errors"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/style"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRuntimePopupPaintsWithinTerminal(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer ConfigureTheme(style.ThemeDark)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		for _, size := range [][2]int{{36, 6}, {60, 12}, {120, 40}} {
			m := Model{width: size[0], height: size[1], runtimeError: "cannot run analyzer: " + strings.Repeat("path/", 100) + "\nlast diagnostic"}
			popup := runtimeErrorPopup(m)
			if lipgloss.Width(popup) > m.width || lipgloss.Height(popup) > m.height {
				t.Fatalf("size%v popup%dx%d", size, lipgloss.Width(popup), lipgloss.Height(popup))
			}
			for y, line := range strings.Split(popup, "\n") {
				for x, bg := range sourceBackgroundCells(line) {
					if !bg {
						t.Fatalf("theme%s size%v unpainted%dx%d", theme, size, x, y)
					}
				}
			}
		}
	}
}

func TestRuntimeFailuresOpenPopup(t *testing.T) {
	for _, message := range []tea.Msg{watcherReady{err: errors.New("fatal diagnostic")}, sourceChange{Err: errors.New("fatal diagnostic")}, analysisResult{full: true, err: errors.New("fatal diagnostic")}} {
		m := Model{width: 80, height: 24}
		m.Update(message)
		frame, ok := m.overlays.Top()
		if !ok || frame.Kind != OverlayRuntimeError || m.runtimeError != "fatal diagnostic" || m.status != "" {
			t.Fatalf("failure not in popup: %+v", m)
		}
	}
}
func TestRuntimePopupOwnsKeysAndPreservesCaller(t *testing.T) {
	m := Model{width: 80, height: 24, help: true}
	showRuntimeError(&m, errors.New(strings.Repeat("line\n", 60)))
	m.runtimeErrorOffset = 5
	showRuntimeError(&m, errors.New(m.runtimeError))
	if m.runtimeErrorOffset != 5 || m.overlays.Len() != 2 {
		t.Fatal("duplicate reset popup")
	}
	assertRuntimeScrollBounds(t, &m)
	m.Update(analysisResult{full: true})
	if m.runtimeError == "" {
		t.Fatal("success erased unread error")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.mainView != MainViewFiles {
		t.Fatal("popup leaked navigation")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	frame, ok := m.overlays.Top()
	if !ok || frame.Kind != OverlayHelp || m.runtimeError != "" {
		t.Fatal("caller not restored")
	}
}
func TestRuntimePopupDefersToShutdown(t *testing.T) {
	m := Model{width: 80, height: 24}
	m.overlays.Push(OverlayShutdown, OverlayCaller{})
	showRuntimeError(&m, errors.New("failure during shutdown"))
	frame, _ := m.overlays.Top()
	if frame.Kind != OverlayShutdown {
		t.Fatal("error covered shutdown")
	}
	m.handleShutdownKey(tea.KeyMsg{Type: tea.KeyEsc})
	frame, _ = m.overlays.Top()
	if frame.Kind != OverlayRuntimeError {
		t.Fatal("deferred error not shown")
	}
	_, cmd := handleRuntimeErrorKey(&m, "ctrl+c")
	if cmd == nil {
		t.Fatal("quit swallowed")
	}
}

func assertRuntimeScrollBounds(t *testing.T, m *Model) {
	t.Helper()
	handleRuntimeErrorKey(m, "end")
	last := m.runtimeErrorOffset
	handleRuntimeErrorKey(m, "down")
	if m.runtimeErrorOffset != last {
		t.Fatal("scroll escaped last page")
	}
	handleRuntimeErrorKey(m, "up")
	if m.runtimeErrorOffset != last-1 {
		t.Fatal("scroll did not move back")
	}
}
