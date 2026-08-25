package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Theme string

const (
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

type palette struct {
	textMuted, textPrimary, accentPositive, accentWarning, accentCritical lipgloss.Color
	scoreWarning, scoreCritical, accentInfo                               lipgloss.Color
	surfaceSelected, surfaceScreen, surfaceTop, surfaceHeader             lipgloss.Color
	surfaceFooter, surfaceModal, surfaceDetailTitle, surfaceDetailBody    lipgloss.Color
	textHeader                                                            lipgloss.Color
}

var darkPalette = palette{
	textMuted: "#668298", textPrimary: "#d5e2eb", accentPositive: "#58e7ad",
	accentWarning: "#f0c765", accentCritical: "#ff8291", scoreWarning: "#f5c451",
	scoreCritical: "#ff6174", accentInfo: "#6fb9e8", surfaceSelected: "#245a78",
	surfaceScreen: "#071019", surfaceTop: "#0a1622", surfaceHeader: "#0b1e2d",
	surfaceFooter: "#061019", surfaceModal: "#0d1d29", surfaceDetailTitle: "#0b1e2d",
	surfaceDetailBody: "#091723", textHeader: "#6f8ca2",
}

var lightPalette = palette{
	textMuted: "#526879", textPrimary: "#182734", accentPositive: "#087a57",
	accentWarning: "#8a6200", accentCritical: "#b42336", scoreWarning: "#956900",
	scoreCritical: "#c22b3c", accentInfo: "#176b9e", surfaceSelected: "#b9def2",
	surfaceScreen: "#f7fafc", surfaceTop: "#e8f1f6", surfaceHeader: "#dceaf2",
	surfaceFooter: "#e5eef3", surfaceModal: "#f1f7fa", surfaceDetailTitle: "#dceaf2",
	surfaceDetailBody: "#f5f9fb", textHeader: "#38596f",
}

var (
	TextMuted          lipgloss.Color
	TextPrimary        lipgloss.Color
	AccentPositive     lipgloss.Color
	AccentWarning      lipgloss.Color
	AccentCritical     lipgloss.Color
	ScoreWarning       lipgloss.Color
	ScoreCritical      lipgloss.Color
	AccentInfo         lipgloss.Color
	SurfaceSelected    lipgloss.Color
	SurfaceScreen      lipgloss.Color
	SurfaceTop         lipgloss.Color
	SurfaceHeader      lipgloss.Color
	SurfaceFooter      lipgloss.Color
	SurfaceModal       lipgloss.Color
	SurfaceDetailTitle lipgloss.Color
	SurfaceDetailBody  lipgloss.Color
	TextHeader         lipgloss.Color
)

func init() { ApplyTheme(ThemeDark) }

func ApplyTheme(theme Theme) {
	selected := darkPalette
	if theme == ThemeLight {
		selected = lightPalette
	}
	TextMuted, TextPrimary = selected.textMuted, selected.textPrimary
	AccentPositive, AccentWarning = selected.accentPositive, selected.accentWarning
	AccentCritical, ScoreWarning = selected.accentCritical, selected.scoreWarning
	ScoreCritical, AccentInfo = selected.scoreCritical, selected.accentInfo
	SurfaceSelected, SurfaceScreen = selected.surfaceSelected, selected.surfaceScreen
	SurfaceTop, SurfaceHeader = selected.surfaceTop, selected.surfaceHeader
	SurfaceFooter, SurfaceModal = selected.surfaceFooter, selected.surfaceModal
	SurfaceDetailTitle, SurfaceDetailBody = selected.surfaceDetailTitle, selected.surfaceDetailBody
	TextHeader = selected.textHeader
}

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
