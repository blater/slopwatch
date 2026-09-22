package follow

import (
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceignore"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"path/filepath"
)

func (model *Model) openFilesExclusions() tea.Cmd {
	document, err := preferences.LoadProject(model.options.Workspace)
	if err != nil {
		showRuntimeError(model, err)
		return nil
	}
	editor := textarea.New()
	field := lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceFieldActive)
	editor.FocusedStyle = textarea.Style{Base: field, Text: field, CursorLine: field, Placeholder: field, EndOfBuffer: field}
	editor.BlurredStyle = editor.FocusedStyle
	editor.Cursor.Style = field
	editor.CharLimit = 0
	editor.MaxHeight = 0
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.Placeholder = "One gitignore pattern per line"
	editor.SetValue(document.Files.Exclude)
	model.runtime.filesExclusions = editor
	model.runtime.filesEditing = true
	model.resizeFilesExclusions()
	return model.runtime.filesExclusions.Focus()
}

func (model Model) filesExclusionsView() string {
	editor := model.runtime.filesExclusions
	return paintSurface(editor.View(), editor.Width(), editor.Height(), style.TextPrimary, style.SurfaceFieldActive)
}

func (model *Model) resizeFilesExclusions() {
	width, height := model.width, model.height
	if width <= 0 {
		width = 64
	}
	if height <= 0 {
		height = 18
	}
	model.runtime.filesExclusions.SetWidth(max(1, min(56, width-8)))
	model.runtime.filesExclusions.SetHeight(max(1, min(10, height-7)))
}

func (model *Model) handleFilesExclusionsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "esc" {
		model.runtime.filesEditing = false
		model.runtime.filesExclusions.Blur()
		return model, nil
	}
	return model, model.updateFilesExclusions(key)
}

func (model *Model) updateFilesExclusions(message tea.Msg) tea.Cmd {
	previous := model.runtime.filesExclusions.Value()
	var command tea.Cmd
	model.runtime.filesExclusions, command = model.runtime.filesExclusions.Update(message)
	if model.runtime.filesExclusions.Value() == previous {
		return command
	}
	document, err := preferences.LoadProject(model.options.Workspace)
	if err == nil {
		document.Files.Exclude = model.runtime.filesExclusions.Value()
		err = preferences.SaveProject(model.options.Workspace, document)
	}
	if err != nil {
		showRuntimeError(model, err)
		return command
	}
	model.status = "Exclusion settings saved; press r to apply"
	return command
}

func (model *Model) toggleGitignore() tea.Cmd {
	model.options.DisableGitignore = !model.options.DisableGitignore
	persistUserPreferences(model)
	model.status = "Ignore settings saved; press r to apply"
	return nil
}

func (model *Model) resumeWatcherWait() tea.Cmd {
	if model.runtime.watchStopped || model.watcher == nil {
		return nil
	}
	return waitForChange(model.watcher)
}

func (model *Model) pruneIgnoredRows() error {
	matcher, err := sourceignore.New(model.options.Workspace, model.options.DisableGitignore)
	if err != nil {
		return err
	}
	keep := make([]report.File, 0, len(model.files.BaseDocument.Files))
	for _, file := range model.files.BaseDocument.Files {
		if !matcher.Ignored(filepath.Join(model.options.Workspace, file.Path), false) {
			keep = append(keep, file)
		} else {
			delete(model.files.Rows, file.Path)
			delete(model.files.Marked, file.Path)
		}
	}
	model.files.BaseDocument.Files = keep
	// Prevent the empty-base fallback from restoring the previous projection.
	model.files.Document.Files = append([]report.File(nil), keep...)
	rebuildWeightedDocument(model)
	restoreSelection(model)
	return nil
}
