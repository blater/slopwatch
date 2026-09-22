package follow

// overlayRenderer owns the routing from the transient overlay stack to each
// feature surface. Keeping this decision tree beside the overlay surface
// helpers leaves Model responsible for lifecycle state, while preserving the
// existing Model method used by the renderer and tests.
type overlayRenderer struct{ model Model }

func (renderer overlayRenderer) render(base string, frame OverlayFrame) string {
	model := renderer.model
	switch frame.Kind {
	case OverlayFixForm:
		return model.overlay(base, fixDialogPopup(model.fixDialog, model.profileCatalog, model.width, model.height))
	case OverlayTargetScoreEditor:
		return fixTargetScoreEditorView(base, model.fixDialog, model.profileCatalog, model.width, model.height)
	case OverlayConfigSettings:
		if fullScreenSurface(model.width, model.height) {
			return configSettingsFullScreen(model.configSettings, model.profileCatalog, model.width, model.height)
		}
		return model.overlay(base, configSettingsPopup(model.configSettings, model.profileCatalog, model.width, model.height))
	case OverlayPromptEditor:
		return masterPromptEditorView(model.configSettings, model.width, model.height)
	case OverlayJobMonitor:
		return jobMonitorView(base, model.jobMonitor, model.width, model.height, model.agentMetricPolicy())
	case OverlayConfirmation:
		if fullScreenSurface(model.width, model.height) {
			return confirmationFullScreen(model.runtime.jobActions.confirmation, model.width, model.height)
		}
		return model.overlay(base, confirmationPopup(model.runtime.jobActions.confirmation, model.width))
	case OverlayJobLog, OverlayJobDiff, OverlayCandidateSource:
		return jobReaderView(base, model.runtime.jobReader, model.width, model.height)
	case OverlaySettingsDirty:
		return dirtyChoiceView(base, "UNSAVED SETTINGS", model.configSettings.dirtyCursor, model.width, model.height)
	case OverlayShutdown:
		return shutdownView(base, model.runtime.shutdown, model.width, model.height)
	case OverlayNotificationLoss:
		return model.overlay(base, notificationLossPopup(model))
	case OverlayRuntimeError:
		return model.overlay(base, runtimeErrorPopup(model))
	default:
		return base
	}
}
