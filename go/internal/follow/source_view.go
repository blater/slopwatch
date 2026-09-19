package follow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/style"
)

func openSourceView(model *Model) tea.Cmd {
	file, ok := model.files.selectedFile(model.options.Limit)
	if !ok {
		return nil
	}
	model.source.loadGeneration++
	generation := model.source.loadGeneration
	model.source.path = file.Path
	model.source.view = true
	model.source.loading = true
	model.source.lastKey = ""
	model.source.lastAt = time.Time{}
	model.source.rapid = 0
	model.source.searchText = ""
	width, height := sourceDimensions(model.width, model.height)
	model.source.viewport = newSourceViewport(width, height)
	model.source.viewport.SetContent("Loading source…")
	model.source.resize(width, height)
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
			err:        err,
			path:       file.Path,
			contents:   string(contents),
			viewport:   prepared,
			highlight:  highlight,
		}
	}
}

func highlightSourceCommand(generation uint64, path, contents string, width, height int, theme style.Theme) tea.Cmd {
	return func() tea.Msg {
		prepared := newSourceViewport(width, height)
		prepared.SetContent(highlightSource(path, contents, theme))
		prepared.GotoTop()
		return sourceHighlighted{generation: generation, path: path, viewport: prepared}
	}
}

func newSourceViewport(width, height int) viewport.Model {
	result := viewport.New(max(1, width-4), max(1, height-4))
	result.SetHorizontalStep(8)
	return result
}

func sourceDimensions(width, height int) (int, int) {
	return min(width, max(10, int(float64(width)*0.96))), min(height, max(5, int(float64(height)*0.94)))
}

func sourceViewView(model Model) string {
	outerWidth, outerHeight := sourceDimensions(model.width, model.height)
	findFooter := ""
	if model.source.findOpen {
		findFooter = model.source.findFooter(max(1, outerWidth-2))
	}
	return model.source.render(outerWidth, outerHeight, findFooter)
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
