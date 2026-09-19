package follow

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/style"
)

func TestCursorHighlightFadesAfterIdlePeriod(t *testing.T) {
	ConfigureTheme(style.ThemeDark)
	defer ConfigureTheme(style.ThemeDark)
	activity := time.Unix(100, 0)
	model := Model{cursorActivity: activity}
	selected := style.SelectionSurface(true)
	normal := style.SelectionSurface(false)
	if got := cursorHighlightBackground(model, selected, normal, activity.Add(10*time.Second)); got != selected {
		t.Fatalf("highlight at idle boundary = %q, want selected %q", got, selected)
	}
	middle := cursorHighlightBackground(model, selected, normal, activity.Add(15*time.Second))
	if middle == selected || middle == normal {
		t.Fatalf("half-faded highlight = %q, want blended colour", middle)
	}
	if got := cursorHighlightBackground(model, selected, normal, activity.Add(20*time.Second)); got != normal {
		t.Fatalf("highlight at fade end = %q, want normal %q", got, normal)
	}
}

func TestCursorHighlightTargetsMarkedBackgroundAndKeyResetsActivity(t *testing.T) {
	ConfigureTheme(style.ThemeLight)
	defer ConfigureTheme(style.ThemeDark)
	activity := time.Unix(200, 0)
	model := Model{cursorActivity: activity}
	marked := rowBackgroundAt(rowState{}, false, true, time.Minute, activity)
	if got := cursorHighlightBackground(model, style.SelectionSurface(true), marked, activity.Add(20*time.Second)); got != marked {
		t.Fatalf("fade end discarded marked background: %q, want %q", got, marked)
	}
	edited := rowBackgroundAt(rowState{editedAt: activity}, false, false, time.Minute, activity)
	if got := cursorHighlightBackground(model, style.SelectionSurface(true), edited, activity.Add(20*time.Second)); got != edited {
		t.Fatalf("fade end discarded edit background: %q, want %q", got, edited)
	}
	if _, _ = handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}); !model.cursorActivity.After(activity) {
		t.Fatal("unhandled key did not reset cursor activity")
	}
	if got := cursorFadeProgress(model.cursorActivity, model.cursorActivity); got != 0 {
		t.Fatalf("key reset left highlight faded: %v", got)
	}
}

func TestCursorHighlightTickUsesFastCadenceOnlyDuringFade(t *testing.T) {
	activity := time.Unix(300, 0)
	model := Model{cursorActivity: activity}
	if got := cursorHighlightTick(model, activity.Add(10*time.Second)); got != 100*time.Millisecond {
		t.Fatalf("fade start tick interval = %s, want 100ms", got)
	}
	if got := cursorHighlightTick(model, activity.Add(15*time.Second)); got != 100*time.Millisecond {
		t.Fatalf("fade tick interval = %s, want 100ms", got)
	}
	model.cursorActivity = activity
	if got := cursorHighlightTick(model, activity.Add(21*time.Second)); got != time.Second {
		t.Fatalf("idle tick interval = %s, want 1s", got)
	}
	model.analyzing = true
	if got := cursorHighlightTick(model, activity.Add(15*time.Second)); got != 125*time.Millisecond {
		t.Fatalf("analysis tick interval = %s, want 125ms", got)
	}
}

func TestCursorHighlightAnimationTickDoesNotResetActivityAndModalKeyDoes(t *testing.T) {
	activity := time.Unix(400, 0)
	model := Model{cursorActivity: activity}
	updated, _ := handleMessage(&model, animationTick(activity.Add(20*time.Second)))
	if result := updated.(*Model); !result.cursorActivity.Equal(activity) {
		t.Fatalf("animation tick changed activity: %v", result.cursorActivity)
	}
	model.overlays.Push(OverlayHelp, OverlayCaller{MainView: MainViewFiles})
	before := model.cursorActivity
	if _, _ = handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}); !model.cursorActivity.After(before) {
		t.Fatal("modal key did not reset cursor activity before dispatch")
	}
}
