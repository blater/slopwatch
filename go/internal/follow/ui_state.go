package follow

// MainView identifies the persistent dashboard surface below transient
// overlays. Each main view owns its own navigation state.
type MainView uint8

const (
	MainViewFiles MainView = iota
	MainViewAgents
)

func (model *Model) switchMainView(next MainView) {
	if next == model.mainView {
		return
	}
	model.mainView = next
	if next == MainViewFiles {
		restoreSelection(model)
		model.clampPathOffset()
	}
}

func (model *Model) toggleMainView() {
	if model.mainView == MainViewAgents {
		model.switchMainView(MainViewFiles)
		return
	}
	model.switchMainView(MainViewAgents)
}

type ResponsiveTier uint8

const (
	ResponsiveResize ResponsiveTier = iota
	ResponsiveCompact
	ResponsiveMedium
	ResponsiveFull
)
