package native

import (
	"github.com/blater/slopmochi/internal/analysiscache"
	"github.com/blater/slopmochi/internal/report"
)

func scoreInputsFromArtifact(artifact analysiscache.UnitArtifact, owned []string, requireCoverage bool) (scoreInputs, bool) {
	inputs := newScoreInputs()
	inputs.diagnostics = append(inputs.diagnostics, artifact.Report.Diagnostics...)
	inputs.plans = append(inputs.plans, artifact.Report.ExecutionPlans...)
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
