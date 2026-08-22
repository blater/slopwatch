package native

import (
	"math"

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
