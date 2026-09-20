package native

import (
	"slices"
	"sort"

	"github.com/blater/slopwatch/internal/report"
)

// scoreDepthComponent keeps nullable v4 boundary states separate from the
// legacy observation stream. Numeric reporting uses every valid known
// boundary value, while completeness continues to describe semantic proof.
func scoreDepthComponent(descriptor componentDescriptor, _ string, depths map[string]report.DepthBoundary, ids []string, pathState, path string) (report.Component, error) {
	boundaryIDs := sortedDepthBoundaryIDs(ids)
	component := report.Component{Subjects: []report.SubjectContribution{}, Waivers: []map[string]any{}, DepthVersion: "responsibility-burden-v4", DepthBoundaryIDs: boundaryIDs}
	boundaries := depthForPath(depths, boundaryIDs)
	component.DepthScope = "file"
	for _, boundary := range boundaries {
		if slices.Contains(boundary.DeclarationFiles, path) {
			zero := 0.0
			component.DepthRole = "declaration-only-contract"
			component.DepthState = "measured"
			component.RawMaximum = &zero
			return applyDepthContribution(descriptor, component)
		}
	}
	component.Observations = len(boundaries)
	component.DeduplicatedObservations = len(boundaries)
	component.Evidence = make([]report.MeasurementEvidence, 0)
	state := pathState
	var maximum *float64
	for _, boundary := range boundaries {
		component.DepthEstimated = component.DepthEstimated || boundary.Estimated
		if state == "" || depthStateRank(boundary.State) > depthStateRank(state) {
			state = boundary.State
		}
		if boundary.State == "not_applicable" {
			continue
		}
		if boundary.Estimated {
			state = mergeDepthState(state, "partial")
		}
		if boundary.Shallow == nil || boundary.State == "unavailable" {
			continue
		}
		if maximum == nil || *boundary.Shallow > *maximum {
			value := *boundary.Shallow
			maximum = &value
		}
		component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: boundary.ID, Value: *boundary.Shallow})
		component.Evidence = append(component.Evidence, depthEvidence(boundary, path))
	}
	if state == "" || (state == "measured" && maximum == nil) {
		state = "unavailable"
	}
	component.DepthState = state
	component.RawMaximum = maximum
	if maximum == nil {
		return component, nil
	}
	return applyDepthContribution(descriptor, component)
}

// The nullable projection is established before applying the existing catalog formula.
func applyDepthContribution(descriptor componentDescriptor, component report.Component) (report.Component, error) {
	threshold, hasThreshold, err := descriptor.Defaults.threshold()
	if err != nil {
		return report.Component{}, err
	}
	weight, err := descriptor.Defaults.weight()
	if err != nil {
		return report.Component{}, err
	}
	value := *component.RawMaximum
	contribution, err := scalarContribution(descriptor.Defaults.Formula, value, threshold, weight, hasThreshold)
	if err != nil {
		return report.Component{}, err
	}
	component.Contribution, component.ObservedContribution = contribution, contribution
	component.ScoringDefinition = &report.ScoringDefinition{Aggregation: "max"}
	for index := range component.Subjects {
		severity, err := scalarSeverity(descriptor.Defaults.Formula, component.Subjects[index].Value, threshold, hasThreshold)
		if err != nil {
			return report.Component{}, err
		}
		component.Subjects[index].BaseSeverity = severity
		component.Subjects[index].Contribution = roundScore(weight * severity)
	}
	return component, nil
}

func depthForPath(depths map[string]report.DepthBoundary, ids []string) []report.DepthBoundary {
	result := make([]report.DepthBoundary, 0)
	for _, id := range ids {
		if boundary, ok := depths[id]; ok {
			result = append(result, boundary)
		}
	}
	return result
}

func sortedDepthBoundaryIDs(ids []string) []string {
	result := append([]string(nil), ids...)
	sort.Strings(result)
	write := 0
	for _, id := range result {
		if id == "" || (write > 0 && result[write-1] == id) {
			continue
		}
		result[write] = id
		write++
	}
	return result[:write]
}

func depthEvidence(boundary report.DepthBoundary, path string) report.MeasurementEvidence {
	return report.MeasurementEvidence{Name: boundary.ID, Symbol: boundary.ID, Scope: depthEvidenceScope(boundary), Value: *boundary.Shallow, Location: report.SourceRange{Path: path}, Attributes: map[string]any{"boundary_id": boundary.ID}}
}

func depthEvidenceScope(boundary report.DepthBoundary) string {
	if boundary.Scope != "" {
		return boundary.Scope
	}
	return "module"
}
