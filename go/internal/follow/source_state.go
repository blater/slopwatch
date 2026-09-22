package follow

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/style"
)

const sourceRepeatWindow = 125 * time.Millisecond

func (state *sourceState) close() {
	state.view = false
	state.loading = false
	state.path = ""
	state.lastKey = ""
	state.lastAt = time.Time{}
	state.rapid = 0
}

func (state *sourceState) resize(width, height int) {
	state.viewport.Width = max(1, width-4)
	state.viewport.Height = max(1, height-4)
	state.viewport.SetHorizontalStep(8)
}

func (state *sourceState) scrollLines(direction string, now time.Time) int {
	lines := 1
	if state.lastKey == direction && !state.lastAt.IsZero() && now.Sub(state.lastAt) <= sourceRepeatWindow {
		state.rapid++
	} else {
		state.rapid = 0
	}
	if state.rapid >= 2 {
		lines = 2
	}
	state.lastKey = direction
	state.lastAt = now
	return lines
}

func (state *sourceState) findNext(direction int) bool {
	query := strings.ToLower(state.findQuery)
	if query == "" {
		return false
	}
	lines := strings.Split(state.searchText, "\n")
	start := state.viewport.YOffset + direction
	for checked := 0; checked < len(lines); checked++ {
		line := ((start+checked*direction)%len(lines) + len(lines)) % len(lines)
		if strings.Contains(strings.ToLower(lines[line]), query) {
			state.viewport.SetYOffset(line)
			return true
		}
	}
	return false
}

func (state sourceState) findFooter(width int) string {
	background := lipgloss.NewStyle().Background(style.SurfaceFooter)
	input := style.InputField(state.findInput.View(), max(8, min(24, width/3)))
	text := background.Render(" FIND "+input+"  ") + hintRow(style.SurfaceFooter,
		hintItem{"ENTER", "find"}, hintItem{"ESC", "cancel"},
	)
	return background.Render(padANSI(truncateANSI(text, width), width))
}

func (state sourceState) render(outerWidth, outerHeight int, findFooter string) string {
	innerWidth := max(1, outerWidth-2)
	headerLeft := "  " + state.path
	headerRight := "loading "
	if !state.loading {
		headerRight = fmt.Sprintf("%d lines ", sourceLineCount(state.searchText))
	}
	headerLeftWidth := max(0, innerWidth-lipgloss.Width(headerRight))
	header := padANSI(truncateANSI(headerLeft, headerLeftWidth), headerLeftWidth) + headerRight
	header = lipgloss.NewStyle().Bold(true).Foreground(style.AccentPositive).Background(style.SurfaceHeader).Render(padANSI(truncateANSI(header, innerWidth), innerWidth))
	body := state.viewport.View()
	bodyLines := strings.Split(body, "\n")
	contentWidth := max(1, innerWidth-2)
	for index, line := range bodyLines {
		bodyLines[index] = paintSourceLine(line, contentWidth, style.TextPrimary, style.SurfaceDetailBody)
	}
	header = paintSourceLine(header, innerWidth, style.AccentPositive, style.SurfaceHeader)
	lines := []string{header}
	lines = append(lines, bodyLines...)
	leftHints := "  " + hintRow(style.SurfaceFooter,
		hintItem{"ctrl-f/b", "page"},
		hintItem{"g/G", "jump"},
	)
	rightItems := []hintItem{{"f", "find"}}
	if state.findQuery != "" {
		rightItems = append(rightItems, hintItem{"n/N", "next"})
	}
	rightItems = append(rightItems, hintItem{"ESC", "close"})
	rightHints := hintRow(style.SurfaceFooter, rightItems...) + lipgloss.NewStyle().Background(style.SurfaceFooter).Render(" ")
	leftWidth := max(0, innerWidth-lipgloss.Width(rightHints))
	footer := padANSI(truncateANSI(leftHints, leftWidth), leftWidth) + rightHints
	if state.findOpen {
		footer = findFooter
	}
	lines = append(lines, paintSourceLine(footer, innerWidth, style.TextMuted, style.SurfaceFooter))
	for len(lines) < outerHeight-2 {
		lines = append(lines, paintSourceLine("", innerWidth, style.TextPrimary, style.SurfaceDetailBody))
	}
	if len(lines) > outerHeight-2 {
		lines = lines[:outerHeight-2]
	}
	return lipgloss.NewStyle().Width(innerWidth).Height(outerHeight - 2).Border(lipgloss.RoundedBorder()).BorderForeground(style.AccentInfo).
		BorderBackground(style.SurfaceDetailBody).Background(style.SurfaceDetailBody).Render(strings.Join(lines, "\n"))
}

func paintSourceLine(line string, width int, foreground, background lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Cut(line, 0, width)
	base := sourceStylePrefix(foreground, background)
	painted := repaintSourceANSI(line, base, sourceForegroundPrefix(foreground), sourceBackgroundPrefix(background))
	used := lipgloss.Width(ansi.Strip(line))
	if used < width {
		painted += "\x1b[0m" + base + strings.Repeat(" ", width-used)
	}
	return painted + "\x1b[0m"
}

func sourceStylePrefix(foreground, background lipgloss.Color) string {
	sample := lipgloss.NewStyle().Foreground(foreground).Background(background).Render(" ")
	return sourceANSIStylePrefix(sample)
}

func sourceForegroundPrefix(foreground lipgloss.Color) string {
	sample := lipgloss.NewStyle().Foreground(foreground).Render(" ")
	return sourceANSIStylePrefix(sample)
}

func sourceBackgroundPrefix(background lipgloss.Color) string {
	sample := lipgloss.NewStyle().Background(background).Render(" ")
	return sourceANSIStylePrefix(sample)
}

func sourceANSIStylePrefix(sample string) string {
	if index := strings.IndexByte(sample, ' '); index >= 0 {
		return sample[:index]
	}
	return ""
}

func repaintSourceANSI(line, base, foreground, background string) string {
	var painted strings.Builder
	painted.WriteString(base)
	for index := 0; index < len(line); {
		if line[index] == '\x1b' && index+1 < len(line) && line[index+1] == '[' {
			if end := strings.IndexByte(line[index+2:], 'm'); end >= 0 {
				end += index + 2
				sequence := line[index : end+1]
				painted.WriteString(sequence)
				state := parseSourceSGRState(strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m"))
				if state.foregroundReset && !state.foregroundSet {
					painted.WriteString(foreground)
				}
				if state.backgroundReset && !state.backgroundSet {
					painted.WriteString(background)
				}
				index = end + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(line[index:])
		if size == 0 {
			break
		}
		painted.WriteString(line[index : index+size])
		index += size
	}
	return painted.String()
}

type sourceSGRFlags struct {
	foregroundSet, backgroundSet, foregroundReset, backgroundReset bool
}

func parseSourceSGRState(parameters string) sourceSGRFlags {
	state := sourceSGRFlags{}
	if parameters == "" {
		state.foregroundReset = true
		state.backgroundReset = true
		return state
	}
	values := strings.Split(parameters, ";")
	for index := 0; index < len(values); index++ {
		code, ok := parseSourceSGRCode(values[index])
		if !ok {
			continue
		}
		switch {
		case code == 0:
			state.foregroundSet = false
			state.backgroundSet = false
			state.foregroundReset = true
			state.backgroundReset = true
		case code == 39:
			state.foregroundSet = false
			state.foregroundReset = true
		case code == 49:
			state.backgroundSet = false
			state.backgroundReset = true
		case code == 38:
			state.foregroundSet = true
			state.foregroundReset = false
			index += sourceSGRExtendedLength(values, index)
		case code == 48:
			state.backgroundSet = true
			state.backgroundReset = false
			index += sourceSGRExtendedLength(values, index)
		case code == 58:
			index += sourceSGRExtendedLength(values, index)
		case code >= 30 && code <= 37 || code >= 90 && code <= 97:
			state.foregroundSet = true
			state.foregroundReset = false
		case code >= 40 && code <= 47 || code >= 100 && code <= 107:
			state.backgroundSet = true
			state.backgroundReset = false
		}
	}
	return state
}

func parseSourceSGRCode(value string) (int, bool) {
	code := 0
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		code = code*10 + int(digit-'0')
	}
	return code, true
}

func sourceSGRExtendedLength(values []string, index int) int {
	if index+1 >= len(values) {
		return 0
	}
	mode, ok := parseSourceSGRCode(values[index+1])
	if !ok {
		return 0
	}
	if mode == 5 {
		return 2
	}
	if mode == 2 {
		return 4
	}
	return 0
}
