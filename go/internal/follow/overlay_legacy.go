package follow

// legacyOverlayInput is the immutable projection of Model state used to
// rebuild the compatibility overlay stack. The stack owns frame ordering and
// replacement; Model only supplies current view and overlay flags.
type legacyOverlayInput struct {
	mainView            MainView
	selected            string
	detail              bool
	source              bool
	help                bool
	columns             bool
	columnsFromSettings bool
	sort                bool
	weights             bool
	appearance          bool
	config              bool
	settings            bool
	info                bool
	find                bool
	findSource          bool
}

// reconcileLegacy preserves the precedence used by the original boolean
// overlays while leaving feature-owned frames untouched. Compatibility frames
// are rebuilt as one chain so callers retain the same view and selection.
func (stack *OverlayStack) reconcileLegacy(input legacyOverlayInput) {
	for _, frame := range stack.frames {
		if !frame.compatibility {
			return
		}
	}

	frames := make([]OverlayFrame, 0, 2)
	caller := OverlayCaller{MainView: input.mainView, Selected: input.selected}
	appendFrame := func(kind OverlayKind) {
		frames = append(frames, OverlayFrame{
			Kind:          kind,
			Caller:        caller,
			compatibility: true,
		})
		caller = OverlayCaller{MainView: input.mainView, Overlay: kind, Selected: input.selected}
	}

	switch {
	case input.detail:
		appendFrame(OverlayDetail)
	case input.source:
		appendFrame(OverlaySource)
	case input.help:
		appendFrame(OverlayHelp)
	case input.columns:
		if input.columnsFromSettings {
			caller.Overlay = OverlaySettings
		}
		appendFrame(OverlayColumns)
	case input.sort:
		appendFrame(OverlaySort)
	case input.weights:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayWeights)
	case input.appearance:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayAppearance)
	case input.config:
		caller.Overlay = OverlaySettings
		appendFrame(OverlayConfigSettings)
	case input.settings:
		appendFrame(OverlaySettings)
	}

	if input.info {
		appendFrame(OverlayInfo)
	}
	if input.find {
		if input.findSource && len(frames) == 0 {
			appendFrame(OverlaySource)
		}
		appendFrame(OverlayFind)
	}
	stack.replace(frames)
}

// reconcileLegacyOverlayStack keeps the public Model integration point stable;
// ordering and frame ownership live with OverlayStack.reconcileLegacy.
func (model *Model) reconcileLegacyOverlayStack() {
	model.overlays.reconcileLegacy(legacyOverlayInput{
		mainView:            model.mainView,
		selected:            model.mainSelection(),
		detail:              model.detail,
		source:              model.source.view,
		help:                model.help,
		columns:             model.columns,
		columnsFromSettings: model.columnsFromSettings,
		sort:                model.sortOpen,
		weights:             model.weightsOpen,
		appearance:          model.appearance,
		config:              model.configSettings.open,
		settings:            model.settings,
		info:                model.infoOpen,
		find:                model.source.findOpen,
		findSource:          model.source.findSource,
	})
}

func (model Model) mainSelection() string {
	if model.mainView == MainViewAgents {
		return model.agents.Selected.String()
	}
	return model.files.Selected
}
