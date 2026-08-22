package follow

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

const newFileIndicatorWindow = 10 * time.Minute

func newFileMarker(rank, total int, state rowState, now time.Time) (string, lipgloss.Color, bool) {
	if state.newFileAt.IsZero() || now.Sub(state.newFileAt) >= newFileIndicatorWindow {
		return "", colourMuted, false
	}
	if !state.newFileMoved {
		return "●", colourGreen, true
	}
	if rank <= topRank(total, 10) {
		return "●", colourRed, true
	}
	if rank <= topRank(total, 50) {
		return "●", colourAmber, true
	}
	return "●", colourGreen, true
}

func topRank(total, percentage int) int {
	if total <= 0 {
		return 1
	}
	return max(1, (total*percentage+99)/100)
}
