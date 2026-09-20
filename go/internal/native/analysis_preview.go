package native

import (
	"context"

	"github.com/blater/slopwatch/internal/report"
)

// Publish ordinary analyzer observations when coverage closes a component.
// No separate progress-result record is required.
func analysisFilePreview(ctx context.Context, request analyzerRequest) func(protocolRecord) error {
	state := analysisProgress(ctx)
	if state == nil {
		return nil
	}
	languages := map[string]string{}
	for _, unit := range request.Units {
		languages[unit.ID] = unit.Language
	}
	pending := map[string]scoreInputs{}
	return func(record protocolRecord) error {
		if record.Type == "analysis_progress" {
			emitProtocolProgress(ctx, record)
			return nil
		}
		if record.Path == nil || !state.allowed[*record.Path] || (record.Type != "measurement" && record.Type != "coverage") {
			return nil
		}
		path := *record.Path
		inputs, exists := pending[path]
		if !exists {
			inputs = newScoreInputs()
		}
		inputs.replaceCompleted = true
		record.Language = languages[record.UnitID]
		if err := inputs.add(record); err != nil {
			return err
		}
		inputs.languages[path] = record.Language
		pending[path] = inputs
		if record.Type != "coverage" {
			return nil
		}
		snapshot := cloneScoreInputs(inputs)
		if run := sourceDepthRunFromContext(ctx); run != nil && run.observePreview(path, snapshot) {
			return nil
		}
		return emitAnalysisFileProvisional(ctx, snapshot, path)
	}
}

func emitProtocolProgress(ctx context.Context, record protocolRecord) {
	state := analysisProgress(ctx)
	if state == nil || record.Language == "" {
		return
	}
	state.emit(report.Document{Progress: map[string]report.ScanProgress{record.Language: {
		Stage: record.Stage, Completed: record.Completed, Total: record.Total, Files: record.Files,
	}}})
}
