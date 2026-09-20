package native

import (
	"context"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

type sourceDepthProgress struct {
	ctx           context.Context
	inputs        *scoreInputs
	truncated     map[string]bool
	shared        map[string]bool
	byBoundary    map[string][]sourceestimate.Result
	finalized     map[string]bool
	provisional   bool
	pendingSource bool
	err           error
}

func (state *sourceDepthProgress) apply(file sourceestimate.File, estimate sourceestimate.Result) error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	inputs := state.inputs
	if file.Language == "go" {
		projectGoFileDepth(inputs, file, !state.truncated[file.Path])
	}
	projectAttributedFileDepth(inputs, file, estimate)
	if state.truncated[file.Path] {
		estimate.Limitations = append(estimate.Limitations, "source_byte_limit")
		if !estimate.Applicable {
			estimate.Applicable, estimate.Burden, estimate.Hidden = true, 1, 1
			estimate.Categories = map[string]float64{"unknown_outcome": 1}
		}
	}
	inputs.languages[file.Path] = file.Language
	if inputs.coverage[file.Path] == nil {
		inputs.coverage[file.Path] = map[string]string{"module_shallowness": "partial"}
	}
	ids := inputs.depthByPath[file.Path]
	if len(ids) == 0 {
		id := "source:" + file.Language + ":" + file.Path
		inputs.depth[id] = report.DepthBoundary{ID: id, State: "partial", Files: []string{file.Path}, Boundary: map[string]any{"artifact": file.Path, "audience": "source", "view": "file", "symbol": file.Path}}
		inputs.depthByPath[file.Path] = []string{id}
		ids = inputs.depthByPath[file.Path]
	}
	for _, id := range ids {
		boundary := inputs.depth[id]
		if !needsSourceDepth(boundary) {
			if boundary.State == "measured" && estimate.Grade != nil && !estimate.RoleOnly && len(boundary.DeclarationFiles) == 0 {
				inputs.depth[id] = gradedMeasuredBoundary(boundary, estimate)
			}
			continue
		}
		boundary.Raw = nil
		inputs.depth[id] = boundary
		state.byBoundary[id] = append(state.byBoundary[id], estimate)
	}
	return state.emitIfReady(file.Path)
}

func (state *sourceDepthProgress) emitIfReady(path string) error {
	progress := analysisProgress(state.ctx)
	if progress == nil {
		return nil
	}
	for _, id := range state.inputs.depthByPath[path] {
		if state.shared[id] {
			return nil
		}
	}
	for _, id := range state.inputs.depthByPath[path] {
		if state.finalized[id] || len(state.byBoundary[id]) == 0 {
			continue
		}
		state.inputs.depth[id] = estimateDepthBoundary(state.inputs.depth[id], sourceestimate.Merge(state.byBoundary[id]...))
		state.finalized[id] = true
	}
	state.inputs.depthStates[path] = sourceDepthState(state.inputs, path)
	if state.provisional {
		return emitAnalysisFilePending(state.ctx, *state.inputs, path, state.pendingSource)
	}
	return emitAnalysisFile(state.ctx, *state.inputs, path)
}

func sourceDepthState(inputs *scoreInputs, path string) string {
	state := ""
	for _, id := range inputs.depthByPath[path] {
		state = mergeDepthState(state, inputs.depth[id].State)
	}
	return state
}

func (state *sourceDepthProgress) finalize(files []sourceestimate.File) {
	for id, estimates := range state.byBoundary {
		if !state.finalized[id] {
			state.inputs.depth[id] = estimateDepthBoundary(state.inputs.depth[id], sourceestimate.Merge(estimates...))
		}
	}
	for _, file := range files {
		state.inputs.depthStates[file.Path] = sourceDepthState(state.inputs, file.Path)
	}
}
