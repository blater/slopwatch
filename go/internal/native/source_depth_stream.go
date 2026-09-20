package native

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

// sourceDepthRun starts the bounded source pass alongside the language
// adapter. Its results are applied once to the adapter's final inputs; the
// source pass is never replayed for every protocol record.
type sourceDepthRun struct {
	ctx     context.Context
	request analyzerRequest
	done    chan struct{}

	mu         sync.Mutex
	emitMu     sync.Mutex
	files      []sourceestimate.File
	fileByPath map[string]sourceestimate.File
	truncated  map[string]bool
	results    map[string]sourceestimate.Result
	preview    map[string]scoreInputs
	previewRev map[string]uint64
	fileCount  int
	err        error
}

type sourceDepthRunKey struct{}

func sourceDepthEnabled(request analyzerRequest) bool {
	for _, component := range request.Components {
		if component.ID == "module_shallowness" && component.Version == ShallowDefinitionV4 {
			return true
		}
	}
	return false
}

func startSourceDepthRun(ctx context.Context, request analyzerRequest) context.Context {
	// The concurrent source pass exists to feed the live progress path. Keep
	// ordinary batch runs on their established single-pass fallback.
	if !analysisProgressRequested(ctx) || !sourceDepthEnabled(request) || sourceDepthRunFromContext(ctx) != nil {
		return ctx
	}
	run := &sourceDepthRun{ctx: ctx, request: request, done: make(chan struct{}), fileByPath: map[string]sourceestimate.File{}, truncated: map[string]bool{}, results: map[string]sourceestimate.Result{}, preview: map[string]scoreInputs{}, previewRev: map[string]uint64{}, fileCount: sourceRequestFileCount(request)}
	if state := analysisProgress(ctx); state != nil {
		state.sourceRun = run
	}
	ctx = context.WithValue(ctx, sourceDepthRunKey{}, run)
	go run.execute()
	return ctx
}

func sourceDepthRunFromContext(ctx context.Context) *sourceDepthRun {
	if run, _ := ctx.Value(sourceDepthRunKey{}).(*sourceDepthRun); run != nil {
		return run
	}
	if state := analysisProgress(ctx); state != nil {
		return state.sourceRun
	}
	return nil
}

func (run *sourceDepthRun) execute() {
	defer close(run.done)
	files, truncated, err := readSourceDepthFiles(run.ctx, run.request)
	run.mu.Lock()
	run.files, run.truncated, run.err = files, truncated, err
	for _, file := range files {
		run.fileByPath[file.Path] = file
	}
	run.mu.Unlock()
	if err != nil || len(files) == 0 {
		return
	}

	_, _ = sourceestimate.AnalyzeWithAttributionProgressAndStatus(files, func(file sourceestimate.File, estimate sourceestimate.Result) {
		if run.ctx.Err() != nil {
			return
		}
		run.mu.Lock()
		run.results[file.Path] = estimate
		_, havePreview := run.preview[file.Path]
		run.mu.Unlock()
		if havePreview {
			run.emitLatest(file.Path)
		}
	}, func(progress sourceestimate.AttributionProgress) {
		if run.ctx.Err() != nil {
			return
		}
		emitSourceEstimateProgress(run.ctx, run.request, progress, run.fileCount)
	})
}

// observePreview retains the latest per-file semantic snapshot while the
// adapter is still running. If bounded attribution is already ready, join it
// immediately for that file instead of waiting for the terminal record.
func (run *sourceDepthRun) observePreview(path string, inputs scoreInputs) bool {
	run.mu.Lock()
	previous, exists := run.preview[path]
	if exists {
		mergePreviewInputs(&previous, inputs)
		run.preview[path] = previous
	} else {
		run.preview[path] = cloneScoreInputs(inputs)
	}
	run.previewRev[path]++
	run.mu.Unlock()
	return run.emitLatest(path)
}

// mergePreviewInputs replaces each delivered component snapshot while keeping
// components that arrived earlier. A component result is not an
// append-only observation log: replaying it must not double score a metric.
func mergePreviewInputs(destination *scoreInputs, incoming scoreInputs) {
	for path, components := range incoming.observations {
		if destination.observations[path] == nil {
			destination.observations[path] = map[string][]observation{}
		}
		for component, values := range components {
			destination.observations[path][component] = append([]observation(nil), values...)
		}
	}
	for path, components := range incoming.coverage {
		if destination.coverage[path] == nil {
			destination.coverage[path] = map[string]string{}
		}
		for component, state := range components {
			destination.coverage[path][component] = state
			if _, hasMeasurements := incoming.observations[path][component]; !hasMeasurements {
				delete(destination.observations[path], component)
			}
		}
	}
	for path, language := range incoming.languages {
		destination.languages[path] = language
	}
	for path, ids := range incoming.depthByPath {
		replacePreviewDepthPath(destination, path, ids)
	}
	for path, components := range incoming.coverage {
		if _, hasDepthCoverage := components["module_shallowness"]; hasDepthCoverage {
			if _, hasDepthRecords := incoming.depthByPath[path]; !hasDepthRecords {
				replacePreviewDepthPath(destination, path, nil)
			}
		}
	}
	for id, boundary := range incoming.depth {
		destination.depth[id] = copyDepthBoundary(boundary)
	}
	for path, state := range incoming.depthStates {
		destination.depthStates[path] = state
	}
	if len(incoming.diagnostics) > 0 {
		destination.diagnostics = append([]map[string]any(nil), incoming.diagnostics...)
	}
	if len(incoming.plans) > 0 {
		destination.plans = append([]map[string]any(nil), incoming.plans...)
	}
}

func replacePreviewDepthPath(inputs *scoreInputs, path string, ids []string) {
	old := inputs.depthByPath[path]
	inputs.depthByPath[path] = append([]string(nil), ids...)
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for _, id := range old {
		if keep[id] {
			continue
		}
		referenced := false
		for otherPath, otherIDs := range inputs.depthByPath {
			if otherPath == path {
				continue
			}
			for _, otherID := range otherIDs {
				if otherID == id {
					referenced = true
					break
				}
			}
			if referenced {
				break
			}
		}
		if !referenced {
			delete(inputs.depth, id)
		}
	}
}

func (run *sourceDepthRun) emitLatest(path string) bool {
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	revision := run.previewRev[path]
	file, haveFile := run.fileByPath[path]
	estimate, haveEstimate := run.results[path]
	preview, havePreview := run.preview[path]
	inputs := cloneScoreInputs(preview)
	run.mu.Unlock()
	if revision == 0 || !havePreview {
		return false
	}
	if !haveFile || !haveEstimate {
		return emitAnalysisFilePending(run.ctx, inputs, path, true) == nil
	}
	pendingSource := inputs.coverage[path]["module_shallowness"] == ""
	progress := &sourceDepthProgress{ctx: run.ctx, inputs: &inputs, truncated: run.truncated, shared: map[string]bool{}, byBoundary: map[string][]sourceestimate.Result{}, finalized: map[string]bool{}, provisional: true, pendingSource: pendingSource}
	return progress.apply(file, estimate) == nil
}

func cloneScoreInputs(source scoreInputs) scoreInputs {
	destination := newScoreInputs()
	for path, components := range source.observations {
		destination.observations[path] = map[string][]observation{}
		for component, values := range components {
			destination.observations[path][component] = append([]observation(nil), values...)
		}
	}
	for path, components := range source.coverage {
		destination.coverage[path] = map[string]string{}
		for component, state := range components {
			destination.coverage[path][component] = state
		}
	}
	for path, language := range source.languages {
		destination.languages[path] = language
	}
	destination.diagnostics = append([]map[string]any(nil), source.diagnostics...)
	destination.plans = append([]map[string]any(nil), source.plans...)
	for id, boundary := range source.depth {
		destination.depth[id] = copyDepthBoundary(boundary)
	}
	for path, ids := range source.depthByPath {
		destination.depthByPath[path] = append([]string(nil), ids...)
	}
	for path, state := range source.depthStates {
		destination.depthStates[path] = state
	}
	return destination
}

func (run *sourceDepthRun) wait() ([]sourceestimate.File, map[string]bool, map[string]sourceestimate.Result, error) {
	select {
	case <-run.done:
	case <-run.ctx.Done():
		return nil, nil, nil, run.ctx.Err()
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	files := append([]sourceestimate.File(nil), run.files...)
	truncated := make(map[string]bool, len(run.truncated))
	for path, value := range run.truncated {
		truncated[path] = value
	}
	results := make(map[string]sourceestimate.Result, len(run.results))
	for path, result := range run.results {
		results[path] = result
	}
	return files, truncated, results, run.err
}

func readSourceDepthFiles(ctx context.Context, request analyzerRequest) ([]sourceestimate.File, map[string]bool, error) {
	const maxSourceBytes = 2 << 20
	files := make([]sourceestimate.File, 0)
	truncated := map[string]bool{}
	seen := map[string]bool{}
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			stream, err := os.Open(filepath.Join(request.Workspace, filepath.FromSlash(path)))
			if err != nil {
				// Preserve adapter diagnostics; an unreadable source cannot
				// honestly receive a source-based estimate.
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(stream, maxSourceBytes+1))
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil {
				continue
			}
			if len(data) > maxSourceBytes {
				truncated[path] = true
				data = data[:maxSourceBytes]
			}
			files = append(files, sourceestimate.File{Path: path, Language: unit.Language, Source: data})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, truncated, nil
}

func emitSourceEstimateProgress(ctx context.Context, request analyzerRequest, progress sourceestimate.AttributionProgress, fileCount int) {
	state := analysisProgress(ctx)
	if state == nil {
		return
	}
	language := progress.Language
	if language == "" && len(request.Units) > 0 {
		language = request.Units[0].Language
	}
	if language == "" {
		return
	}
	state.emit(report.Document{Progress: map[string]report.ScanProgress{language: {
		Stage: progress.Stage, Completed: progress.Completed, Total: progress.Total, Files: fileCount,
	}}})
}

func sourceRequestFileCount(request analyzerRequest) int {
	paths := map[string]bool{}
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			paths[path] = true
		}
	}
	return len(paths)
}
