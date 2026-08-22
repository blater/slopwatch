package follow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (model *Model) openSourceView() {
	file, ok := model.selectedFile()
	if !ok {
		return
	}
	model.sourcePath = file.Path
	model.sourceView = true
	model.sourceLastKey = ""
	model.sourceLastAt = time.Time{}
	model.sourceRapid = 0
	model.sourceSearchText = ""
	width, height := model.sourceDimensions()
	model.sourceViewport = viewport.New(max(1, width-4), max(1, height-4))
	model.resizeSourceViewport()
	path := filepath.Join(model.options.Workspace, filepath.FromSlash(file.Path))
	contents, err := os.ReadFile(path)
	if err != nil {
		model.sourceViewport.SetContent(fmt.Sprintf("Unable to read %s: %v", file.Path, err))
		return
	}
	model.sourceViewport.SetContent(highlightSource(file.Path, string(contents)))
	model.sourceSearchText = string(contents)
	model.sourceViewport.GotoTop()
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

func (model Model) sourceViewView() string {
	outerWidth, outerHeight := model.sourceDimensions()
	innerWidth := max(1, outerWidth-2)
	header := fmt.Sprintf("  SOURCE  %s", model.sourcePath)
	closeLabel := "ESC / Q CLOSE  "
	if lipgloss.Width(header)+lipgloss.Width(closeLabel) <= innerWidth {
		header += strings.Repeat(" ", innerWidth-lipgloss.Width(header)-lipgloss.Width(closeLabel)) + closeLabel
	}
	header = lipgloss.NewStyle().Bold(true).Foreground(colourGreen).Background(headerBackground).Render(padANSI(truncateANSI(header, innerWidth), innerWidth))
	body := model.sourceViewport.View()
	bodyLines := strings.Split(body, "\n")
	contentWidth := max(1, innerWidth-2)
	for index, line := range bodyLines {
		bodyLines[index] = lipgloss.NewStyle().Foreground(colourText).Background(lipgloss.Color("#091723")).Render(padANSI(ansi.Cut(line, 0, contentWidth), contentWidth))
	}
	lines := []string{header}
	lines = append(lines, bodyLines...)
	separator := lipgloss.NewStyle().Background(footerBackground).Render("  ")
	footer := "  " + keyHint("ctrl-f/b", " page", footerBackground) + separator +
		keyHint("g/G", " jump", footerBackground) + separator +
		keyHint("f", " find", footerBackground) + separator +
		keyHint("n/N", " next", footerBackground)
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
	return lipgloss.NewStyle().Width(innerWidth).Height(outerHeight - 2).Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).Render(strings.Join(lines, "\n"))
}
