package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// wrapText wraps words to width and allows callers to align continuation
// lines with a different prefix. Prefixes count toward the line width.
func wrapText(text string, width int, firstPrefix, continuationPrefix string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{firstPrefix}
	}
	lines := []string{}
	prefix := firstPrefix
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		limit := max(1, width-lipgloss.Width(prefix))
		if current != "" && lipgloss.Width(candidate) > limit {
			lines = append(lines, prefix+current)
			prefix = continuationPrefix
			current = word
		} else {
			current = candidate
		}
	}
	lines = append(lines, prefix+current)
	return lines
}
