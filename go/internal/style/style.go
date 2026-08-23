package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	TextMuted          = lipgloss.Color("#668298")
	TextPrimary        = lipgloss.Color("#d5e2eb")
	AccentPositive     = lipgloss.Color("#58e7ad")
	AccentWarning      = lipgloss.Color("#f0c765")
	AccentCritical     = lipgloss.Color("#ff8291")
	ScoreWarning       = lipgloss.Color("#f5c451")
	ScoreCritical      = lipgloss.Color("#ff6174")
	AccentInfo         = lipgloss.Color("#6fb9e8")
	SurfaceSelected    = lipgloss.Color("#245a78")
	SurfaceScreen      = lipgloss.Color("#071019")
	SurfaceTop         = lipgloss.Color("#0a1622")
	SurfaceHeader      = lipgloss.Color("#0b1e2d")
	SurfaceFooter      = lipgloss.Color("#061019")
	SurfaceModal       = lipgloss.Color("#0d1d29")
	SurfaceDetailTitle = lipgloss.Color("#0b1e2d")
	SurfaceDetailBody  = lipgloss.Color("#091723")
	TextHeader         = lipgloss.Color("#6f8ca2")
)

func Heading(text string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(AccentInfo).Render(text)
}

func SelectionSurface(selected bool) lipgloss.Color {
	if selected {
		return SurfaceSelected
	}
	return SurfaceScreen
}

func ModalOption(text string, selected bool, width int) string {
	background := SelectionSurface(selected)
	return lipgloss.NewStyle().Width(width).Background(background).Foreground(TextPrimary).Render(text)
}

func DisabledOption(text string, width int) string {
	return lipgloss.NewStyle().Width(width).Background(SurfaceScreen).Foreground(TextMuted).Render(text)
}

func SortOption(arrow, label string, active, cursor bool, width int) string {
	background := SelectionSurface(cursor)
	arrowColour := TextMuted
	labelColour := TextMuted
	if cursor {
		labelColour = TextPrimary
	}
	if active {
		arrowColour = AccentWarning
		if arrow == "▼" {
			arrowColour = AccentPositive
		}
	}
	arrowText := lipgloss.NewStyle().Foreground(arrowColour).Background(background).Bold(active).Render(arrow)
	labelText := lipgloss.NewStyle().Foreground(labelColour).Background(background).Render(" " + label)
	return lipgloss.NewStyle().Width(width).Background(background).Render(arrowText + labelText)
}

func ModalBox(content string, width int) string {
	return Popup("", strings.Split(content, "\n"), "", width)
}

func Popup(header string, content []string, footer string, width int) string {
	lines := make([]string, 0, len(content)+4)
	if header != "" {
		lines = append(lines, header)
	}
	lines = append(lines, "")
	lines = append(lines, content...)
	if footer != "" {
		lines = append(lines, "", footer)
	} else {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(AccentInfo).Background(SurfaceModal).Foreground(TextPrimary).Render(strings.Join(lines, "\n"))
}

func StripLeadingSpace(text string) string {
	return strings.TrimLeft(text, " ")
}
