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
	OverlayTargetScoreEditor
	OverlayPromptEditor
	OverlayJobMonitor
	OverlayJobLog
	OverlayJobDiff
	OverlayCandidateSource
	OverlayConfirmation
	OverlaySettingsDirty
	OverlayShutdown
	OverlayNotificationLoss
	OverlayRuntimeError
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
