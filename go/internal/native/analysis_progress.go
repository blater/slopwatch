package native

import (
	"context"
	"sync"

	"github.com/blater/slopwatch/internal/report"
)

type analysisProgressKey struct{}

type analysisProgressState struct {
	mu          sync.Mutex
	emit        func(report.Document)
	catalog     catalogDocument
	allowed     map[string]bool
	passScore   *float64
	diagnostics bool
	sourceRun   *sourceDepthRun
}

// WithAnalysisProgress attaches a callback for finalized file projections.
// Callbacks may run concurrently for different language workers.
func WithAnalysisProgress(ctx context.Context, emit func(report.Document)) context.Context {
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, analysisProgressKey{}, emit)
}

func configureAnalysisProgress(ctx context.Context, catalog catalogDocument, allowed map[string]bool, passScore *float64) context.Context {
	if _, ok := ctx.Value(analysisProgressKey{}).(*analysisProgressState); ok {
		return ctx
	}
	emit, _ := ctx.Value(analysisProgressKey{}).(func(report.Document))
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, analysisProgressKey{}, &analysisProgressState{emit: emit, catalog: catalog, allowed: allowed, passScore: passScore})
}

func analysisProgress(ctx context.Context) *analysisProgressState {
	state, _ := ctx.Value(analysisProgressKey{}).(*analysisProgressState)
	return state
}

func analysisProgressRequested(ctx context.Context) bool {
	if analysisProgress(ctx) != nil {
		return true
	}
	_, ok := ctx.Value(analysisProgressKey{}).(func(report.Document))
	return ok
}

func emitAnalysisFile(ctx context.Context, inputs scoreInputs, path string) error {
	return emitAnalysisFileMode(ctx, inputs, path, false)
}

func emitAnalysisFileProvisional(ctx context.Context, inputs scoreInputs, path string) error {
	return emitAnalysisFileMode(ctx, inputs, path, true, false)
}

func emitAnalysisFilePending(ctx context.Context, inputs scoreInputs, path string, sourcePending bool) error {
	return emitAnalysisFileMode(ctx, inputs, path, true, sourcePending)
}

func emitAnalysisFileMode(ctx context.Context, inputs scoreInputs, path string, provisional bool, sourcePending ...bool) error {
	state := analysisProgress(ctx)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	allowed := state.allowed[path]
	catalog := state.catalog
	passScore := state.passScore
	state.mu.Unlock()
	if !allowed {
		return nil
	}
	language := inputs.languages[path]
	file, err := scoreFile(path, language, catalog.Components, inputs.observations, inputs.coverage, inputs.depth, inputs.depthByPath, inputs.depthStates, passScore)
	if err != nil {
		return err
	}
	if provisional {
		file.PendingComponents = pendingComponents(file, catalog.Components)
		if len(sourcePending) > 0 && sourcePending[0] && hasComponent(catalog.Components, "module_shallowness", language) {
			file.PendingComponents = appendUnique(file.PendingComponents, "module_shallowness")
		}
	} else {
		file.PendingComponents = nil
	}
	depth := make(map[string]report.DepthBoundary, len(inputs.depthByPath[path]))
	for _, id := range inputs.depthByPath[path] {
		if boundary, ok := inputs.depth[id]; ok {
			depth[id] = copyDepthBoundary(boundary)
		}
	}
	document := report.Document{Files: []report.File{file}, Depth: depth}
	state.mu.Lock()
	includeDiagnostics := !state.diagnostics
	if includeDiagnostics {
		state.diagnostics = true
	}
	state.mu.Unlock()
	if includeDiagnostics {
		document.Diagnostics = append([]map[string]any(nil), inputs.diagnostics...)
	}
	state.emit(document)
	return nil
}

func hasComponent(descriptors []componentDescriptor, id, language string) bool {
	for _, descriptor := range descriptors {
		if descriptor.ID == id && descriptor.Defaults.Enabled && descriptor.supported(language) {
			return true
		}
	}
	return false
}

func pendingComponents(file report.File, descriptors []componentDescriptor) []string {
	pending := make([]string, 0)
	for _, descriptor := range descriptors {
		if !descriptor.Defaults.Enabled || !descriptor.supported(file.Language) {
			continue
		}
		// A received unavailable/partial/failed state is finished
		// calculation with evidence limitations. Only an absent coverage
		// record is still pending work.
		if _, received := file.Coverage[descriptor.ID]; !received {
			pending = append(pending, descriptor.ID)
		}
	}
	return pending
}

func copyDepthBoundary(boundary report.DepthBoundary) report.DepthBoundary {
	boundary.DeclarationFiles = append([]string(nil), boundary.DeclarationFiles...)
	boundary.Dependencies = append([]string(nil), boundary.Dependencies...)
	boundary.Files = append([]string(nil), boundary.Files...)
	boundary.Reasons = append([]any(nil), boundary.Reasons...)
	boundary.PreciseReasons = append([]any(nil), boundary.PreciseReasons...)
	boundary.Evidence = append([]any(nil), boundary.Evidence...)
	boundary.Boundary = copyAnyMap(boundary.Boundary)
	boundary.Raw = copyAnyMap(boundary.Raw)
	if boundary.Shallow != nil {
		value := *boundary.Shallow
		boundary.Shallow = &value
	}
	return boundary
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
