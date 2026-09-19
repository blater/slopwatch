package follow

import "context"

// runtimeState groups transient coordination and retry state that does not
// belong to a particular dashboard surface. Event handlers access this
// lifecycle cluster through Model.runtime.
type runtimeState struct {
	watchGeneration         uint64
	watchReconfigurePending bool
	watchReconfigureCancel  context.CancelFunc
	watchNeedsWait          bool
	watchRetryCount         int
	startupWatcherPending   bool
	fixGeneration           uint64
	jobActions              jobActionState
	jobReader               jobReaderState
	shutdown                shutdownState
	targetScorePreference   targetScorePreferenceState
	filesSettings           bool
	configParent            *configSettingsState
	runtimeErrorMessages    []string
	fixErrorSummary         string
	runtimeErrorOffset      int
	columnsFromSettings     bool
	pendingFullAnalysis     bool
	discardAnalysis         bool
	analysisRetryPending    bool
	weightsResetConfirm     bool
}
