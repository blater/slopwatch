package follow

import (
	"fmt"
	"strings"
	"time"

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
		bodyLines[index] = lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceDetailBody).Render(padANSI(ansi.Cut(line, 0, contentWidth), contentWidth))
	}
	lines := []string{header}
	lines = append(lines, bodyLines...)
	leftHints := "  " + hintRow(style.SurfaceFooter,
		hintItem{"ctrl-f/b", "page"},
		hintItem{"g/G", "jump"},
	)
	rightHints := hintRow(style.SurfaceFooter,
		hintItem{"f", "find"},
		hintItem{"n/N", "next"},
		hintItem{"ESC", "close"},
	) + lipgloss.NewStyle().Background(style.SurfaceFooter).Render(" ")
	leftWidth := max(0, innerWidth-lipgloss.Width(rightHints))
	footer := padANSI(truncateANSI(leftHints, leftWidth), leftWidth) + rightHints
	if state.findOpen {
		footer = findFooter
	}
	lines = append(lines, padANSI(truncateANSI(footer, innerWidth), innerWidth))
	for len(lines) < outerHeight-2 {
		lines = append(lines, strings.Repeat(" ", innerWidth))
	}
	if len(lines) > outerHeight-2 {
		lines = lines[:outerHeight-2]
	}
	return lipgloss.NewStyle().Width(innerWidth).Height(outerHeight - 2).Border(lipgloss.RoundedBorder()).BorderForeground(style.AccentInfo).Render(strings.Join(lines, "\n"))
}
