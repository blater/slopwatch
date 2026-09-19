package follow

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	cursorIdleDelay  = 10 * time.Second
	cursorFadeLength = 10 * time.Second
)

func cursorHighlightBackground(model Model, selected, normal lipgloss.Color, now time.Time) lipgloss.Color {
	progress := cursorFadeProgress(model.cursorActivity, now)
	if progress <= 0 {
		return selected
	}
	if progress >= 1 {
		return normal
	}
	return blendCursorColours(selected, normal, progress)
}

func cursorFadeProgress(activity, now time.Time) float64 {
	if activity.IsZero() || now.Before(activity.Add(cursorIdleDelay)) {
		return 0
	}
	if !now.Before(activity.Add(cursorIdleDelay + cursorFadeLength)) {
		return 1
	}
	return float64(now.Sub(activity.Add(cursorIdleDelay))) / float64(cursorFadeLength)
}

func blendCursorColours(from, to lipgloss.Color, progress float64) lipgloss.Color {
	progress = min(1, max(0, progress))
	fromComponents := colourComponents(from)
	toComponents := colourComponents(to)
	blend := func(left, right float64) uint8 {
		return uint8(left*(1-progress) + right*progress + 0.5)
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", blend(fromComponents[0], toComponents[0]), blend(fromComponents[1], toComponents[1]), blend(fromComponents[2], toComponents[2])))
}

func cursorFadeActive(activity, now time.Time) bool {
	if activity.IsZero() {
		return false
	}
	elapsed := now.Sub(activity)
	return elapsed >= cursorIdleDelay && elapsed < cursorIdleDelay+cursorFadeLength
}

func cursorHighlightTick(model Model, now time.Time) time.Duration {
	if model.analyzing {
		return 125 * time.Millisecond
	}
	if cursorFadeActive(model.cursorActivity, now) {
		return 100 * time.Millisecond
	}
	return time.Second
}
