package native

import (
	"math"
	"sort"
	"strconv"

	"github.com/blater/slopwatch/internal/report"
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
	baseline, err := descriptor.Defaults.baseline()
	if err != nil {
		return report.Component{}, err
	}
	component.ScoringDefinition = &report.ScoringDefinition{Aggregation: descriptor.Aggregator}
	if descriptor.Kind == "count" {
		if descriptor.ID == "deeply_nested_if" {
			return scoreNestedCount(descriptor, raw, threshold, hasThreshold, weight)
		}
		value := float64(len(raw))
		baseSeverity, err := scalarSeverityWithBaseline(descriptor.Defaults.Formula, value, baseline, threshold, hasThreshold)
		if err != nil {
			return report.Component{}, err
		}
		contribution := roundScore(weight * baseSeverity)
		component.Contribution, component.ObservedContribution = contribution, contribution
		if len(raw) > 0 {
			component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: "deduplicated_count", Value: value, Contribution: contribution, BaseSeverity: baseSeverity})
		}
		return component, nil
	}
	for _, item := range raw {
		var contribution, baseSeverity float64
		var err error
		if descriptor.Kind == "compound" {
			baseSeverity, err = godSeverity(item)
		} else {
			baseSeverity, err = scalarSeverityWithBaseline(descriptor.Defaults.Formula, item.value, baseline, threshold, hasThreshold)
		}
		if err != nil {
			return report.Component{}, err
		}
		contribution = roundScore(weight * baseSeverity)
		if descriptor.Aggregator == "max" {
			component.Contribution = math.Max(component.Contribution, contribution)
		} else {
			component.Contribution += contribution
		}
		subject := subjectKey(item.subject)
		routine := item.scoringRoutine
		// Function observations already use their canonical range key as the
		// subject. Avoid storing that same long key twice in the compact cache.
		if routine == subject {
			routine = ""
		}
		component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: subject, Value: item.value, Contribution: contribution, BaseSeverity: baseSeverity, Routine: routine})
	}
	component.Contribution = roundScore(component.Contribution)
	component.ObservedContribution = component.Contribution
	return component, nil
}

func scoreNestedCount(descriptor componentDescriptor, raw []observation, threshold float64, hasThreshold bool, weight float64) (report.Component, error) {
	component := report.Component{Observations: len(raw), DeduplicatedObservations: len(raw), Subjects: []report.SubjectContribution{}, Waivers: []map[string]any{}, Evidence: make([]report.MeasurementEvidence, 0, len(raw)), ScoringDefinition: &report.ScoringDefinition{Aggregation: descriptor.Aggregator}}
	counts := map[string]int{}
	for _, item := range raw {
		evidence := measurementEvidence(item)
		component.Evidence = append(component.Evidence, evidence)
		key := item.scoringRoutine
		if key == "" {
			key = "unassociated:" + evidence.Location.Path + ":" + strconv.Itoa(evidence.Location.Start.Line) + ":" + strconv.Itoa(evidence.Location.Start.Column)
		}
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := float64(counts[key])
		baseSeverity, err := scalarSeverityWithBaseline(descriptor.Defaults.Formula, value, 0, threshold, hasThreshold)
		if err != nil {
			return report.Component{}, err
		}
		contribution := roundScore(weight * baseSeverity)
		component.Contribution += contribution
		// Subject already carries the enclosing routine key.
		component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: key, Value: value, Contribution: contribution, BaseSeverity: baseSeverity})
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
