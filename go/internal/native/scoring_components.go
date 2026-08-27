package native

import (
	"math"

	"github.com/blater/slopmochi/internal/report"
)

func scoreComponent(descriptor componentDescriptor, state string, raw []observation) (report.Component, error) {
	component := report.Component{Observations: len(raw), DeduplicatedObservations: len(raw), Subjects: []report.SubjectContribution{}, Waivers: []map[string]any{}}
	if state != "complete" {
		return component, nil
	}
	weight, err := descriptor.Defaults.weight()
	if err != nil {
		return report.Component{}, err
	}
	component.Evidence = make([]report.MeasurementEvidence, 0, len(raw))
	for _, item := range raw {
		component.Evidence = append(component.Evidence, measurementEvidence(item))
	}
	threshold, hasThreshold, err := descriptor.Defaults.threshold()
	if err != nil {
		return report.Component{}, err
	}
	if descriptor.Kind == "count" {
		value := float64(len(raw))
		contribution, err := scalarContribution(descriptor.Defaults.Formula, value, threshold, weight, hasThreshold)
		if err != nil {
			return report.Component{}, err
		}
		component.Contribution, component.ObservedContribution = contribution, contribution
		if len(raw) > 0 {
			component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: "deduplicated_count", Value: value, Contribution: contribution})
		}
		return component, nil
	}
	for _, item := range raw {
		var contribution float64
		var err error
		if descriptor.Kind == "compound" {
			contribution, err = godContribution(item, weight)
		} else {
			contribution, err = scalarContribution(descriptor.Defaults.Formula, item.value, threshold, weight, hasThreshold)
		}
		if err != nil {
			return report.Component{}, err
		}
		if descriptor.Aggregator == "max" {
			component.Contribution = math.Max(component.Contribution, contribution)
		} else {
			component.Contribution += contribution
		}
		component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: subjectKey(item.subject), Value: item.value, Contribution: contribution})
	}
	component.Contribution = roundScore(component.Contribution)
	component.ObservedContribution = component.Contribution
	return component, nil
}

func measurementEvidence(item observation) report.MeasurementEvidence {
	start, end := subjectPositions(item.subject)
	symbol := item.subject.Symbol
	if symbol == "" {
		symbol = item.subject.Name
	}
	routine := item.subject.Routine
	if routine == "" && item.scope == "function" {
		routine = symbol
	}
	if routine == "" {
		if value, ok := item.attributes["routine_symbol"].(string); ok {
			routine = value
		}
	}
	return report.MeasurementEvidence{
		Name: item.subject.Name, Symbol: symbol, Routine: routine, Scope: item.scope, Value: item.value,
		Location: report.SourceRange{
			Path:  item.path,
			Start: report.SourcePosition{Line: start.Line, Column: start.Column, Offset: start.Offset},
			End:   report.SourcePosition{Line: end.Line, Column: end.Column, Offset: end.Offset},
		},
		Attributes: item.attributes, Provenance: item.provenance,
	}
}
