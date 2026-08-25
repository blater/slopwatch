package follow

type OverlayKind uint8

const (
	OverlayNone OverlayKind = iota
	OverlayFind
	OverlayInfo
	OverlayHelp
	OverlayDetail
	OverlaySource
	OverlayColumns
	OverlaySort
	OverlayWeights
	OverlayAppearance
	OverlaySettings
	OverlayConfigSettings
	OverlayFixForm
	OverlayPromptEditor
	OverlayPromptDetach
	OverlayPromptDirty
	OverlayJobMonitor
	OverlayJobActions
	OverlayJobLog
	OverlayJobDiff
	OverlayCandidateSource
	OverlayConfirmation
	OverlaySettingsDirty
	OverlayShutdown
)

type OverlayCaller struct {
	MainView MainView
	Overlay  OverlayKind
	Selected string
}

type OverlayFrame struct {
	Kind          OverlayKind
	Caller        OverlayCaller
	compatibility bool
}

type OverlayStack struct {
	frames []OverlayFrame
}

func (stack OverlayStack) Len() int { return len(stack.frames) }

func (stack OverlayStack) Top() (OverlayFrame, bool) {
	if len(stack.frames) == 0 {
		return OverlayFrame{}, false
	}
	return stack.frames[len(stack.frames)-1], true
}

func (stack *OverlayStack) Push(kind OverlayKind, caller OverlayCaller) {
	stack.frames = append(stack.frames, OverlayFrame{Kind: kind, Caller: caller})
}

func (stack *OverlayStack) Pop() (OverlayFrame, bool) {
	if len(stack.frames) == 0 {
		return OverlayFrame{}, false
	}
	index := len(stack.frames) - 1
	frame := stack.frames[index]
	stack.frames = stack.frames[:index]
	return frame, true
}

func (stack *OverlayStack) replace(frames []OverlayFrame) {
	stack.frames = append(stack.frames[:0], frames...)
}

// reconcileLegacyOverlayStack contains the existing boolean dialog state
// behind the new typed routing boundary. Feature overlays can replace this
// bridge incrementally without adding another precedence chain.
func (model *Model) reconcileLegacyOverlayStack() {
	for _, frame := range model.overlays.frames {
		if !frame.compatibility {
			return
		}
	}
	frames := make([]OverlayFrame, 0, 2)
	caller := OverlayCaller{MainView: model.mainView, Selected: model.mainSelection()}
	appendFrame := func(kind OverlayKind) {
		frames = append(frames, OverlayFrame{Kind: kind, Caller: caller, compatibility: true})
		caller = OverlayCaller{MainView: model.mainView, Overlay: kind, Selected: model.mainSelection()}
	}

	switch {
	case model.detail:
		appendFrame(OverlayDetail)
	case model.sourceView:
		appendFrame(OverlaySource)
	case model.help:
		appendFrame(OverlayHelp)
	case model.columns:
		if model.columnsFromSettings {
			caller.Overlay = OverlaySettings
		}
		appendFrame(OverlayColumns)
	case model.sortOpen:
		appendFrame(OverlaySort)
	case model.weightsOpen:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayWeights)
	case model.appearance:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayAppearance)
	case model.configSettings.open:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayConfigSettings)
	case model.settings:
		appendFrame(OverlaySettings)
	}
	if model.infoOpen {
		appendFrame(OverlayInfo)
	}
	if model.findOpen {
		if model.findSource && len(frames) == 0 {
			appendFrame(OverlaySource)
		}
		appendFrame(OverlayFind)
	}
	model.overlays.replace(frames)
}

func (model Model) mainSelection() string {
	if model.mainView == MainViewAgents {
		return model.agents.Selected.String()
	}
	return model.selected
}
