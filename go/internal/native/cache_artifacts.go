package native

import (
	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func scoreInputsFromArtifact(artifact analysiscache.UnitArtifact, owned []string, requireCoverage bool) (scoreInputs, bool) {
	for _, diagnostic := range artifact.Report.Diagnostics {
		if transientAnalyzerDiagnostic(diagnostic) {
			return newScoreInputs(), false
		}
	}
	inputs := newScoreInputs()
	inputs.diagnostics = append(inputs.diagnostics, artifact.Report.Diagnostics...)
	inputs.plans = append(inputs.plans, artifact.Report.ExecutionPlans...)
	for id, boundary := range artifact.Report.Depth {
		// Compact boundaries loaded from older artifacts before they enter the
		// score graph. This preserves typed metadata and proof fields while
		// avoiding another full report payload per cached boundary.
		inputs.depth[id] = report.CompactDepthBoundary(boundary)
	}
	for _, file := range artifact.Report.Files {
		addArtifactFile(&inputs, file)
	}
	inputs = filterScoreInputs(inputs, pathSet(owned))
	return inputs, !requireCoverage || hasOwnedCoverage(inputs, owned)
}

func addArtifactFile(inputs *scoreInputs, file report.File) {
	inputs.languages[file.Path] = file.Language
	inputs.coverage[file.Path] = cloneCoverage(file.Coverage)
	for componentID, component := range file.Components {
		if component.DepthVersion == "responsibility-burden-v4" {
			inputs.depthStates[file.Path] = mergeDepthState(inputs.depthStates[file.Path], component.DepthState)
			continue
		}
		for _, evidence := range component.Evidence {
			path := evidence.Location.Path
			if path == "" {
				path = file.Path
			}
			observations := ensureObservationComponents(inputs.observations[path])
			observations[componentID] = append(observations[componentID], observation{
				component: componentID, path: path, language: file.Language,
				scope: evidence.Scope, value: evidence.Value,
				subject: protocolSubject{
					Name: evidence.Name, Symbol: evidence.Symbol, Routine: evidence.Routine,
					Start: protocolPosition{Line: evidence.Location.Start.Line, Column: evidence.Location.Start.Column, Offset: evidence.Location.Start.Offset},
					End:   protocolPosition{Line: evidence.Location.End.Line, Column: evidence.Location.End.Column, Offset: evidence.Location.End.Offset},
				},
				attributes: evidence.Attributes, provenance: evidence.Provenance,
			})
			inputs.observations[path] = observations
		}
	}
}

func ensureObservationComponents(value map[string][]observation) map[string][]observation {
	if value == nil {
		return map[string][]observation{}
	}
	return value
}

func filterScoreInputs(inputs scoreInputs, allowed map[string]bool) scoreInputs {
	result := newScoreInputs()
	for path, components := range inputs.observations {
		if allowed[path] {
			result.observations[path] = components
		}
	}
	for path, coverage := range inputs.coverage {
		if allowed[path] {
			result.coverage[path] = coverage
		}
	}
	for path, language := range inputs.languages {
		if allowed[path] {
			result.languages[path] = language
		}
	}
	result.diagnostics = append(result.diagnostics, inputs.diagnostics...)
	result.plans = append(result.plans, inputs.plans...)
	for id, boundary := range inputs.depth {
		boundary.Files = intersectPaths(boundary.Files, allowed)
		if len(boundary.Files) > 0 {
			result.depth[id] = boundary
		}
	}
	for path, state := range inputs.depthStates {
		if allowed[path] {
			result.depthStates[path] = state
		}
	}
	for id, boundary := range result.depth {
		for _, path := range boundary.Files {
			result.depthByPath[path] = appendUnique(result.depthByPath[path], id)
		}
	}
	return result
}

func hasOwnedCoverage(inputs scoreInputs, owned []string) bool {
	for _, path := range owned {
		if _, exists := inputs.coverage[path]; !exists {
			return false
		}
	}
	return true
}

func cloneCoverage(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// Terminal failures can describe transient adapter/helper failures even when
// individual paths carry failed coverage. Retry them without requiring a source edit.
func transientAnalyzerDiagnostic(diagnostic map[string]any) bool {
	switch diagnostic["code"] {
	case "native.analyzer_failed", "native.terminal_failure":
		return true
	default:
		return false
	}
}
