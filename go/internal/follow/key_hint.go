package follow

import (
	"strings"

	"github.com/blater/slopmochi/internal/style"
	"github.com/charmbracelet/lipgloss"
)

type hintItem struct {
	key   string
	label string
}

// hintRow renders a complete hint row. Labels are normalized here so every
// hint uses the same two-column spacing and multi-word labels use hyphens.
func hintRow(background lipgloss.Color, items ...hintItem) string {
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.Join(strings.Fields(item.label), "-")
		labelText := label
		if !strings.Contains(strings.ToLower(label), strings.ToLower(item.key)) {
			labelText = " " + label
		}
		rendered = append(rendered, keyHint(item.key, labelText, background))
	}
	return strings.Join(rendered, lipgloss.NewStyle().Background(background).Render("  "))
}

// keyHint emphasizes the first occurrence of key inside label, preserving the
// label's normal spelling. If key is not part of the label, it is rendered as
// a separate key prefix; this supports controls such as CTRL-F/B and Esc.
func keyHint(key, label string, background lipgloss.Color) string {
	keyStyle := lipgloss.NewStyle().Foreground(style.AccentInfo).Background(background).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(style.TextMuted).Background(background)
	keyLower := strings.ToLower(key)
	labelLower := strings.ToLower(label)
	position := strings.Index(labelLower, keyLower)
	if position >= 0 {
		end := position + len(key)
		return labelStyle.Render(label[:position]) + keyStyle.Render(label[position:end]) + labelStyle.Render(label[end:])
	}
	return keyStyle.Render(key) + labelStyle.Render(label)
}
