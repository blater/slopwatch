package follow

import "github.com/blater/slopwatch/internal/fix"

// MainView identifies the persistent dashboard surface below transient
// overlays. Each main view owns its own navigation state.
type MainView uint8

const (
	MainViewFiles MainView = iota
	MainViewAgents
)

type FilesState struct {
	Selected         string
	Cursor           int
	Offset           int
	HorizontalOffset int
	SortKey          string
	SortReverse      bool
	FindQuery        string
}

type AgentsState struct {
	Jobs             []fix.JobPresentation
	Selected         AgentRowID
	Offset           int
	HorizontalOffset int
	SortKey          string
	SortReverse      bool
	FindQuery        string
	FindEditing      bool
	ShowAll          bool
	Expanded         map[fix.JobID]bool
}

func (model *Model) switchMainView(next MainView) {
	if next == model.mainView {
		return
	}
	if model.mainView == MainViewFiles {
		model.captureFilesState()
	}
	model.mainView = next
	if next == MainViewFiles {
		model.restoreFilesState()
	}
}

func (model *Model) toggleMainView() {
	if model.mainView == MainViewAgents {
		model.switchMainView(MainViewFiles)
		return
	}
	model.switchMainView(MainViewAgents)
}

func (model *Model) captureFilesState() {
	model.files = FilesState{
		Selected: model.selected, Cursor: model.cursor, Offset: model.offset,
		HorizontalOffset: model.pathOffset,
		SortKey:          model.sortKey, SortReverse: model.sortReverse,
		FindQuery: model.findQuery,
	}
}

func (model *Model) restoreFilesState() {
	model.selected = model.files.Selected
	model.cursor = model.files.Cursor
	model.offset = model.files.Offset
	model.pathOffset = model.files.HorizontalOffset
	model.sortKey = model.files.SortKey
	model.sortReverse = model.files.SortReverse
	model.findQuery = model.files.FindQuery
	model.restoreSelection()
	model.clampPathOffset()
}

type ResponsiveTier uint8

const (
	ResponsiveResize ResponsiveTier = iota
	ResponsiveCompact
	ResponsiveMedium
	ResponsiveFull
)

func responsiveTier(width, height int) ResponsiveTier {
	if width < 36 || height < 6 {
		return ResponsiveResize
	}
	if width < 60 {
		return ResponsiveCompact
	}
	if width < 96 {
		return ResponsiveMedium
	}
	return ResponsiveFull
}

func fullScreenSurface(width, height int) bool {
	return width < 60 || height < 16
}

func resizeView(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, height)
	for index := range lines {
		lines[index] = padANSI("", width)
	}
	lines[0] = padANSI(truncate("RESIZE TERMINAL", width), width)
	if height > 1 {
		lines[1] = padANSI(truncate("Need at least 36 columns x 6 rows", width), width)
	}
	return joinScreenLines(lines)
}
