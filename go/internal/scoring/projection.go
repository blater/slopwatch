package scoring

import (
	"math"

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
	file.Components = make(map[string]report.Component, len(original.Components))
	file.Axes = map[string]float64{}
	file.Score = 0
	for id, originalComponent := range original.Components {
		component, axis := ProjectComponent(id, originalComponent, policy)
		file.Components[id] = component
		file.Axes[axis] += component.Contribution
	}
	for axis, value := range file.Axes {
		file.Axes[axis] = Round(value)
		file.Score += file.Axes[axis]
	}
	file.Score = Round(file.Score)
	file.ValidZero = file.Complete && file.Score == 0
	return file
}

// ProjectComponent scales one component and its subject contributions.
func ProjectComponent(id string, original report.Component, policy Policy) (report.Component, string) {
	component := original
	component.Subjects = append([]report.SubjectContribution(nil), original.Subjects...)
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
