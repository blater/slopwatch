package follow

import (
	"time"

	"github.com/blater/slopwatch/internal/style"

	"github.com/charmbracelet/lipgloss"
)

const newFileIndicatorWindow = 10 * time.Minute

func newFileMarker(rank, total int, state rowState, now time.Time) (string, lipgloss.Color, bool) {
	if state.newFileAt.IsZero() || now.Sub(state.newFileAt) >= newFileIndicatorWindow {
		return "", style.TextMuted, false
	}
	if !state.newFileMoved {
		return "●", style.AccentPositive, true
	}
	if rank <= topRank(total, 10) {
		return "●", style.AccentCritical, true
	}
	if rank <= topRank(total, 50) {
		return "●", style.AccentWarning, true
	}
	return "●", style.AccentPositive, true
}

func topRank(total, percentage int) int {
	if total <= 0 {
		return 1
	}
	return max(1, (total*percentage+99)/100)
}
