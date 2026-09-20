package scoring

import (
	"math"
	"sort"

	"github.com/blater/slopwatch/internal/report"
)

// Policy is an immutable scoring-policy snapshot. Missing values resolve to
// catalog defaults; an explicitly configured zero weight remains zero.
type Policy struct {
	weights map[string]float64
	enabled map[string]bool
}

// NewPolicy copies the supplied maps so later preference or UI mutations
// cannot alter an in-flight projection.
func NewPolicy(weights map[string]float64, enabled map[string]bool) Policy {
	return Policy{weights: cloneFloatMap(weights), enabled: cloneBoolMap(enabled)}
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	result := make(map[string]bool, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

// Weight returns the configured or default weight for id.
func (policy Policy) Weight(id string) float64 {
	if value, ok := policy.weights[id]; ok {
		return value
	}
	return DefaultWeight(id)
}

// Enabled returns the configured or default enablement for id. Unknown
// components are disabled until they are deliberately added to the catalog.
func (policy Policy) Enabled(id string) bool {
	if value, ok := policy.enabled[id]; ok {
		return value
	}
	component, ok := ComponentByID(id)
	return ok && component.DefaultOn
}

// WeightFactor returns the multiplier applied to observed contributions.
func (policy Policy) WeightFactor(id string) float64 {
	base := DefaultWeight(id)
	if base <= 0 || !policy.Enabled(id) {
		return 0
	}
	return policy.Weight(id) / base
}

// ProjectDocument applies policy and re-ranks a report without mutating its
// files or components.
func ProjectDocument(original report.Document, policy Policy) report.Document {
	document := original
	document.Summary = make(map[string]any, len(original.Summary))
	for key, value := range original.Summary {
		document.Summary[key] = value
	}
	// Like File.Passed, aggregate outcomes belong to native threshold
	// evaluation. Do not publish the previous policy's result after reweighting.
	delete(document.Summary, "passed")
	delete(document.Summary, "failed_files")
	document.Files = ProjectFiles(original.Files, policy)
	document.SortAndRank()
	return document
}

// ProjectFiles applies policy to each file in order.
func ProjectFiles(originals []report.File, policy Policy) []report.File {
	files := make([]report.File, 0, len(originals))
	for _, original := range originals {
		files = append(files, ProjectFile(original, policy))
	}
	return files
}

// ProjectFile recalculates component contributions, axes, and SCORE under the
// supplied policy.
func ProjectFile(original report.File, policy Policy) report.File {
	if len(original.Components) == 0 {
		return original
	}
	file := original
	hadObserved := len(original.ObservedAxes) > 0 || original.ObservedScore != 0
	file.Components = make(map[string]report.Component, len(original.Components))
	file.Axes = map[string]float64{}
	file.Score = 0
	file.ScoringAttributions = nil
	file.ScoringLimitations = append([]string(nil), original.ScoringLimitations...)
	inputsPresent := false
	componentIDs := make([]string, 0, len(original.Components))
	for id := range original.Components {
		componentIDs = append(componentIDs, id)
	}
	sort.Strings(componentIDs)
	for _, component := range original.Components {
		if component.ScoringDefinition != nil {
			inputsPresent = true
			break
		}
	}
	for _, id := range componentIDs {
		originalComponent := original.Components[id]
		component, axis := ProjectComponent(id, originalComponent, policy)
		if inputsPresent && isControlFlow(id) && originalComponent.ScoringDefinition == nil && originalComponent.Contribution != 0 {
			file.ScoringLimitations = appendUniqueString(file.ScoringLimitations, "legacy control-flow contribution retained without compact scoring inputs: "+id)
		}
		file.Components[id] = component
		if originalComponent.ScoringDefinition == nil || !isControlFlow(id) {
			file.Axes[axis] += component.Contribution
		}
	}
	if !inputsPresent {
		file.ScoringLimitations = appendUniqueString(file.ScoringLimitations, "legacy report lacks compact scoring inputs; existing contributions retained")
	}
	if inputsPresent {
		groups := projectControlFlow(&file, policy)
		applyTypeCyclomaticResiduals(&file, original, policy, groups)
		file.Axes = map[string]float64{}
		file.Score = 0
		for _, id := range componentIDs {
			component := file.Components[id]
			axis := ComponentAxis(id)
			if component.Axis != "" {
				axis = component.Axis
			}
			file.Axes[axis] = Round(file.Axes[axis] + component.Contribution)
		}
	}
	axisIDs := make([]string, 0, len(file.Axes))
	for axis := range file.Axes {
		axisIDs = append(axisIDs, axis)
	}
	sort.Strings(axisIDs)
	for _, axis := range axisIDs {
		value := file.Axes[axis]
		file.Axes[axis] = Round(value)
		file.Score += file.Axes[axis]
	}
	file.Score = Round(file.Score)
	// Projection has no pass threshold. A prior native outcome is stale once
	// policy changes the score; native scoring assigns Passed after projection.
	file.Passed = nil
	if !hadObserved {
		file.ObservedAxes = cloneFloatMap(file.Axes)
		file.ObservedScore = file.Score
	}
	file.ValidZero = file.Complete && file.Score == 0
	return file
}

// ProjectComponent scales one component and its subject contributions.
func ProjectComponent(id string, original report.Component, policy Policy) (report.Component, string) {
	component := original
	component.Subjects = append([]report.SubjectContribution(nil), original.Subjects...)
	if original.ScoringDefinition != nil {
		component.Contribution = 0
		for index := range component.Subjects {
			projected := projectedSubject(component.Subjects[index], policy, id)
			component.Subjects[index].Contribution = projected
			if isControlFlow(id) {
				continue
			}
			if original.ScoringDefinition.Aggregation == "max" {
				component.Contribution = math.Max(component.Contribution, projected)
			} else {
				component.Contribution += projected
			}
		}
		component.Contribution = Round(component.Contribution)
		return component, ComponentAxis(id)
	}
	factor := policy.WeightFactor(id)
	component.Contribution = Round(component.Contribution * factor)
	component.ObservedContribution = original.ObservedContribution
	for index := range component.Subjects {
		component.Subjects[index].Contribution = Round(component.Subjects[index].Contribution * factor)
	}
	return component, ComponentAxis(id)
}

// Round uses the precision historically used by dashboard reweighting.
func Round(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}
