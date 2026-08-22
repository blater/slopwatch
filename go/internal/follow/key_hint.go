package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keyHint emphasizes the first occurrence of key inside label, preserving the
// label's normal spelling. If key is not part of the label, it is rendered as
// a separate key prefix; this supports controls such as CTRL-F/B and Esc.
func keyHint(key, label string, background lipgloss.Color) string {
	keyStyle := lipgloss.NewStyle().Foreground(colourBlue).Background(background).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colourMuted).Background(background)
	keyLower := strings.ToLower(key)
	labelLower := strings.ToLower(label)
	position := strings.Index(labelLower, keyLower)
	if position >= 0 {
		end := position + len(key)
		return labelStyle.Render(label[:position]) + keyStyle.Render(label[position:end]) + labelStyle.Render(label[end:])
	}
	return keyStyle.Render(key) + labelStyle.Render(label)
}
