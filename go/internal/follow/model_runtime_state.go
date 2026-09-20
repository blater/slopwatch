package follow

import (
	"context"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/report"
)

type analysisProgressBuffer struct {
	previous    []report.File
	mu          sync.Mutex
	files       map[string]report.File
	depth       map[string]report.DepthBoundary
	progress    map[string]report.ScanProgress
	diagnostics []map[string]any
	lastFlush   time.Time
	generation  uint64
	freshness   report.Freshness
	note        string
	active      bool
}

func newAnalysisProgressBuffer(generation uint64, freshness report.Freshness, note string) *analysisProgressBuffer {
	return &analysisProgressBuffer{files: map[string]report.File{}, depth: map[string]report.DepthBoundary{}, lastFlush: time.Now(), generation: generation, freshness: freshness, note: note, active: true}
}

func (buffer *analysisProgressBuffer) add(document report.Document) {
	buffer.mu.Lock()
	if !buffer.active {
		buffer.mu.Unlock()
		return
	}
	for _, file := range document.Files {
		if file.Path != "" {
			buffer.files[file.Path] = file
		}
	}
	for id, boundary := range document.Depth {
		buffer.depth[id] = boundary
	}
	if buffer.progress == nil {
		buffer.progress = map[string]report.ScanProgress{}
	}
	for language, progress := range document.Progress {
		if progress.Files == 0 {
			progress.Files = buffer.progress[language].Files
		}
		buffer.progress[language] = progress
	}
	buffer.diagnostics = append(buffer.diagnostics, document.Diagnostics...)
	buffer.mu.Unlock()
}

func (buffer *analysisProgressBuffer) deactivate() {
	buffer.mu.Lock()
	buffer.active = false
	buffer.files = map[string]report.File{}
	buffer.depth = map[string]report.DepthBoundary{}
	buffer.diagnostics = nil
	buffer.progress = nil
	buffer.mu.Unlock()
}

func (buffer *analysisProgressBuffer) take(now time.Time, force bool) report.Document {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if !buffer.active || (len(buffer.files) == 0 && len(buffer.depth) == 0 && len(buffer.diagnostics) == 0 && len(buffer.progress) == 0) || (!force && !buffer.lastFlush.IsZero() && now.Sub(buffer.lastFlush) < analysisProgressBatchInterval) {
		return report.Document{}
	}
	result := report.Document{Files: make([]report.File, 0, len(buffer.files)), Depth: buffer.depth, Diagnostics: buffer.diagnostics, Progress: buffer.progress}
	for _, file := range buffer.files {
		if file.Freshness == "" || file.Freshness == report.FreshnessCurrent {
			file.Freshness = buffer.freshness
			file.FreshnessNote = buffer.note
		}
		result.Files = append(result.Files, file)
	}
	buffer.files = map[string]report.File{}
	buffer.depth = map[string]report.DepthBoundary{}
	buffer.diagnostics = nil
	buffer.progress = nil
	buffer.lastFlush = now
	return result
}

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
	scanProgress            map[string]report.ScanProgress
	scanFiles               map[string]bool
	scanFinished            int
	scanTotal               int
	analysisGeneration      uint64
	analysisProgress        *analysisProgressBuffer
	weightsResetConfirm     bool
}
