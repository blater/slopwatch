package follow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/style"
)

func openSourceView(model *Model) tea.Cmd {
	file, ok := model.selectedFile()
	if !ok {
		return nil
	}
	model.sourceLoadGeneration++
	generation := model.sourceLoadGeneration
	model.sourcePath = file.Path
	model.sourceView = true
	model.sourceLoading = true
	model.sourceLastKey = ""
	model.sourceLastAt = time.Time{}
	model.sourceRapid = 0
	model.sourceSearchText = ""
	width, height := model.sourceDimensions()
	model.sourceViewport = newSourceViewport(width, height)
	model.sourceViewport.SetContent("Loading source…")
	model.resizeSourceViewport()
	path := filepath.Join(model.options.Workspace, filepath.FromSlash(file.Path))
	return func() tea.Msg {
		contents, err := os.ReadFile(path)
		text := string(contents)
		highlight := false
		if err != nil {
			text = fmt.Sprintf("Unable to read %s: %v", file.Path, err)
			contents = nil
		} else if sourceGrammarScope(file.Path) != "" {
			highlight = true
		}
		prepared := newSourceViewport(width, height)
		prepared.SetContent(text)
		prepared.GotoTop()
		return sourceLoaded{
			generation: generation,
			path:       file.Path,
			contents:   string(contents),
			viewport:   prepared,
			highlight:  highlight,
		}
	}
}

func highlightSourceCommand(generation uint64, path, contents string, width, height int) tea.Cmd {
	return func() tea.Msg {
		prepared := newSourceViewport(width, height)
		prepared.SetContent(highlightSource(path, contents))
		prepared.GotoTop()
		return sourceHighlighted{generation: generation, path: path, viewport: prepared}
	}
}

func newSourceViewport(width, height int) viewport.Model {
	result := viewport.New(max(1, width-4), max(1, height-4))
	result.SetHorizontalStep(8)
	return result
}

func (model *Model) resizeSourceViewport() {
	width, height := model.sourceDimensions()
	model.sourceViewport.Width = max(1, width-4)
	model.sourceViewport.Height = max(1, height-4)
	// Bubble Tea's viewport disables horizontal movement unless a step is set.
	// Keep it small enough to make narrow-terminal navigation precise.
	model.sourceViewport.SetHorizontalStep(8)
}

func (model Model) sourceDimensions() (int, int) {
	return min(model.width, max(10, int(float64(model.width)*0.96))), min(model.height, max(5, int(float64(model.height)*0.94)))
}

func sourceViewView(model Model) string {
	outerWidth, outerHeight := model.sourceDimensions()
	innerWidth := max(1, outerWidth-2)
	headerLeft := "  " + model.sourcePath
	headerRight := "loading "
	if !model.sourceLoading {
		headerRight = fmt.Sprintf("%d lines ", sourceLineCount(model.sourceSearchText))
	}
	headerLeftWidth := max(0, innerWidth-lipgloss.Width(headerRight))
	header := padANSI(truncateANSI(headerLeft, headerLeftWidth), headerLeftWidth) + headerRight
	header = lipgloss.NewStyle().Bold(true).Foreground(style.AccentPositive).Background(style.SurfaceHeader).Render(padANSI(truncateANSI(header, innerWidth), innerWidth))
	body := model.sourceViewport.View()
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
	if model.findOpen {
		footer = model.findFooter(innerWidth)
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

func sourceLineCount(contents string) int {
	if contents == "" {
		return 0
	}
	lines := strings.Count(contents, "\n")
	if !strings.HasSuffix(contents, "\n") {
		lines++
	}
	return lines
}
