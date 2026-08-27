package style

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Theme string

const (
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

type palette struct {
	textMuted, textPrimary, accentPositive, accentWarning, accentCritical                lipgloss.Color
	scoreWarning, scoreCritical, accentInfo                                              lipgloss.Color
	surfaceSelected, surfaceMarked, surfaceScreen, surfaceTop, surfaceHeader             lipgloss.Color
	surfaceFooter, surfaceModal, surfaceDetailTitle, surfaceDetailBody                   lipgloss.Color
	surfaceField, surfaceFieldActive, surfaceFieldIndicator, surfaceFieldIndicatorActive lipgloss.Color
	textHeader                                                                           lipgloss.Color
}

var darkPalette = palette{
	textMuted: "#668298", textPrimary: "#d5e2eb", accentPositive: "#58e7ad",
	accentWarning: "#f0c765", accentCritical: "#ff8291", scoreWarning: "#f5c451",
	scoreCritical: "#ff6174", accentInfo: "#6fb9e8", surfaceSelected: "#245a78", surfaceMarked: "#142f42",
	surfaceScreen: "#071019", surfaceTop: "#0a1622", surfaceHeader: "#0b1e2d",
	surfaceFooter: "#061019", surfaceModal: "#0d1d29", surfaceDetailTitle: "#0b1e2d",
	surfaceDetailBody: "#091723", surfaceField: "#163246", surfaceFieldActive: "#2d6684",
	surfaceFieldIndicator: "#2b607b", surfaceFieldIndicatorActive: "#4387a8", textHeader: "#6f8ca2",
}

var lightPalette = palette{
	textMuted: "#526879", textPrimary: "#182734", accentPositive: "#087a57",
	accentWarning: "#8a6200", accentCritical: "#b42336", scoreWarning: "#956900",
	scoreCritical: "#c22b3c", accentInfo: "#176b9e", surfaceSelected: "#b9def2", surfaceMarked: "#dcecf4",
	surfaceScreen: "#f7fafc", surfaceTop: "#e8f1f6", surfaceHeader: "#dceaf2",
	surfaceFooter: "#e5eef3", surfaceModal: "#f1f7fa", surfaceDetailTitle: "#dceaf2",
	surfaceDetailBody: "#f5f9fb", surfaceField: "#d8e9f2", surfaceFieldActive: "#a7d0e2",
	surfaceFieldIndicator: "#a9cfdf", surfaceFieldIndicatorActive: "#7fb8d0", textHeader: "#38596f",
}

var (
	TextMuted                   lipgloss.Color
	TextPrimary                 lipgloss.Color
	AccentPositive              lipgloss.Color
	AccentWarning               lipgloss.Color
	AccentCritical              lipgloss.Color
	ScoreWarning                lipgloss.Color
	ScoreCritical               lipgloss.Color
	AccentInfo                  lipgloss.Color
	SurfaceSelected             lipgloss.Color
	SurfaceMarked               lipgloss.Color
	SurfaceScreen               lipgloss.Color
	SurfaceTop                  lipgloss.Color
	SurfaceHeader               lipgloss.Color
	SurfaceFooter               lipgloss.Color
	SurfaceModal                lipgloss.Color
	SurfaceDetailTitle          lipgloss.Color
	SurfaceDetailBody           lipgloss.Color
	SurfaceField                lipgloss.Color
	SurfaceFieldActive          lipgloss.Color
	SurfaceFieldIndicator       lipgloss.Color
	SurfaceFieldIndicatorActive lipgloss.Color
	TextHeader                  lipgloss.Color
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
	SurfaceSelected, SurfaceMarked, SurfaceScreen = selected.surfaceSelected, selected.surfaceMarked, selected.surfaceScreen
	SurfaceTop, SurfaceHeader = selected.surfaceTop, selected.surfaceHeader
	SurfaceFooter, SurfaceModal = selected.surfaceFooter, selected.surfaceModal
	SurfaceDetailTitle, SurfaceDetailBody = selected.surfaceDetailTitle, selected.surfaceDetailBody
	SurfaceField, SurfaceFieldActive = selected.surfaceField, selected.surfaceFieldActive
	SurfaceFieldIndicator, SurfaceFieldIndicatorActive = selected.surfaceFieldIndicator, selected.surfaceFieldIndicatorActive
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

// FormFieldRow renders a form label and its editable value as distinct visual
// regions. Combo fields add the conventional down-pointing indicator directly
// before the value.
func FormFieldRow(label, value string, width, labelWidth int, selected, combo bool) string {
	if width < 1 {
		return ""
	}
	labelWidth = min(max(max(1, labelWidth), lipgloss.Width(label)+1), width)
	rowBackground := SelectionSurface(selected)
	label = ansi.Truncate(label, labelWidth, "")
	labelText := lipgloss.NewStyle().Width(labelWidth).Background(rowBackground).Foreground(TextPrimary).Render(label)
	remaining := max(0, width-labelWidth)
	if remaining == 0 {
		return labelText
	}
	indicator := ""
	if combo {
		indicatorBackground := SurfaceFieldIndicator
		if selected {
			indicatorBackground = SurfaceFieldIndicatorActive
		}
		indicator = lipgloss.NewStyle().Background(indicatorBackground).Foreground(AccentInfo).Bold(true).Render("▼")
		remaining--
	}
	value = ansi.Truncate(value, remaining, "")
	fieldBackground := SurfaceField
	if selected {
		fieldBackground = SurfaceFieldActive
	}
	field := lipgloss.NewStyle().Background(fieldBackground).Foreground(TextPrimary).Bold(selected).Render(value)
	used := labelWidth + lipgloss.Width(indicator) + lipgloss.Width(field)
	remainder := lipgloss.NewStyle().Width(max(0, width-used)).Background(rowBackground).Render("")
	return labelText + indicator + field + remainder
}

func InputField(value string, width int) string {
	width = max(1, width)
	return lipgloss.NewStyle().Background(SurfaceFieldActive).Foreground(TextPrimary).Render(ansi.Truncate(value, width, ""))
}

// ApplyTextInputStyle gives every editable text control the global field
// surface. Call it again while rendering so a live theme change is reflected.
func ApplyTextInputStyle(input *textinput.Model, active bool) {
	background := SurfaceField
	if active {
		background = SurfaceFieldActive
	}
	input.TextStyle = lipgloss.NewStyle().Foreground(TextPrimary).Background(background)
	input.PromptStyle = lipgloss.NewStyle().Foreground(TextPrimary).Background(background)
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(TextMuted).Background(background)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(background).Background(AccentInfo)
}

func ToggleOption(mark, label string, selected, disabled bool, width int) string {
	width = max(1, width)
	rowBackground := SelectionSurface(selected)
	mark = ansi.Truncate(mark, width, "")
	markWidth := lipgloss.Width(mark)
	markText := lipgloss.NewStyle().Background(rowBackground).Foreground(TextPrimary).Bold(selected).Render(mark)
	foreground := TextPrimary
	if disabled {
		foreground = TextMuted
	}
	separatorWidth := min(1, max(0, width-markWidth))
	separator := lipgloss.NewStyle().Width(separatorWidth).Background(rowBackground).Render("")
	labelWidth := max(0, width-markWidth-separatorWidth)
	labelText := lipgloss.NewStyle().Width(labelWidth).Background(rowBackground).Foreground(foreground).Render(ansi.Truncate(label, labelWidth, ""))
	return markText + separator + labelText
}

func ToggleValueOption(mark, value, label string, selected bool, width, valueWidth int) string {
	width = max(1, width)
	rowBackground := SelectionSurface(selected)
	indent := lipgloss.NewStyle().Width(min(4, width)).Background(rowBackground).Render("")
	remaining := max(0, width-lipgloss.Width(indent))
	mark = ansi.Truncate(mark, remaining, "")
	markText := lipgloss.NewStyle().Background(rowBackground).Foreground(TextPrimary).Bold(selected).Render(mark)
	remaining -= lipgloss.Width(markText)
	gap := lipgloss.NewStyle().Width(min(2, remaining)).Background(rowBackground).Render("")
	remaining -= lipgloss.Width(gap)
	value = ansi.Truncate(value, min(max(1, valueWidth), remaining), "")
	valueBackground := SurfaceField
	if selected {
		valueBackground = SurfaceFieldActive
	}
	valueText := lipgloss.NewStyle().Background(valueBackground).Foreground(TextPrimary).Bold(selected).Render(value)
	remaining -= lipgloss.Width(valueText)
	separator := lipgloss.NewStyle().Width(min(3, remaining)).Background(rowBackground).Render("")
	remaining -= lipgloss.Width(separator)
	labelText := lipgloss.NewStyle().Width(remaining).Background(rowBackground).Foreground(TextPrimary).Render(ansi.Truncate(label, remaining, ""))
	return indent + markText + gap + valueText + separator + labelText
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
	arrowBackground := SurfaceField
	if cursor {
		arrowBackground = SurfaceFieldActive
	}
	arrowText := lipgloss.NewStyle().Foreground(arrowColour).Background(arrowBackground).Bold(active).Render(arrow)
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

// TightPopup keeps the same dialog styling without decorative blank rows. It
// is used when the terminal is too short for Popup's spacious layout.
func TightPopup(header string, content []string, footer string, width int) string {
	lines := make([]string, 0, len(content)+2)
	if header != "" {
		lines = append(lines, header)
	}
	lines = append(lines, content...)
	if footer != "" {
		lines = append(lines, footer)
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).
		BorderForeground(AccentInfo).Background(SurfaceModal).Foreground(TextPrimary).Render(strings.Join(lines, "\n"))
}

func StripLeadingSpace(text string) string {
	return strings.TrimLeft(text, " ")
}
