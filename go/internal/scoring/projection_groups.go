package scoring

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/report"
)

var controlFlowComponents = map[string]bool{
	"cognitive_complexity":         true,
	"cyclomatic_method_complexity": true,
	"npath_complexity":             true,
	"deeply_nested_if":             true,
}

func isControlFlow(id string) bool { return controlFlowComponents[id] }

func projectControlFlow(file *report.File, policy Policy) map[string]float64 {
	charges := map[string]float64{}
	for componentID, component := range file.Components {
		if !isControlFlow(componentID) || component.ScoringDefinition == nil {
			continue
		}
		for subjectIndex := range component.Subjects {
			component.Subjects[subjectIndex].Contribution = 0
		}
		file.Components[componentID] = component
	}
	type signal struct {
		component string
		value     float64
		subject   string
		index     int
	}
	groups := map[string][]signal{}
	for componentID, component := range file.Components {
		if !isControlFlow(componentID) || component.ScoringDefinition == nil {
			continue
		}
		for index, subject := range component.Subjects {
			group := projectionGroupKey(componentID, subject, index)
			value := projectedSubject(subject, policy, componentID)
			if value == 0 {
				continue
			}
			groups[group] = append(groups[group], signal{component: componentID, value: value, subject: subject.Subject, index: index})
		}
	}
	groupKeys := make([]string, 0, len(groups))
	for group := range groups {
		groupKeys = append(groupKeys, group)
	}
	sort.Strings(groupKeys)
	for _, group := range groupKeys {
		signals := groups[group]
		sort.SliceStable(signals, func(left, right int) bool {
			if signals[left].value != signals[right].value {
				return signals[left].value > signals[right].value
			}
			if signals[left].component != signals[right].component {
				return signals[left].component < signals[right].component
			}
			return signals[left].subject < signals[right].subject
		})
		winner := signals[0]
		charges[group] = winner.value
		file.Components[winner.component] = addComponentContribution(file.Components[winner.component], winner.value)
		component := file.Components[winner.component]
		if winner.index < len(component.Subjects) {
			component.Subjects[winner.index].Contribution = winner.value
		}
		file.Components[winner.component] = component
		if strings.HasPrefix(group, "unresolved:") || strings.HasPrefix(group, "unassociated:") {
			file.ScoringLimitations = appendUniqueString(file.ScoringLimitations, "control-flow evidence could not be associated with an unambiguous routine: "+group)
		}
		attribution := report.GroupAttribution{Group: group, Component: winner.component, Contribution: winner.value}
		// The attribution already carries the winner; retain only supporting
		// signals here so every routine does not serialize it twice.
		for _, item := range signals[1:] {
			attribution.Signals = append(attribution.Signals, report.GroupSignal{Component: item.component, Contribution: item.value})
		}
		file.ScoringAttributions = append(file.ScoringAttributions, attribution)
	}
	return charges
}

func projectionGroupKey(componentID string, subject report.SubjectContribution, index int) string {
	if subject.Routine != "" {
		return subject.Routine
	}
	if subject.Subject != "" {
		return subject.Subject
	}
	return fmt.Sprintf("unassociated:%s:%d", componentID, index)
}

func projectedSubject(subject report.SubjectContribution, policy Policy, componentID string) float64 {
	if !policy.Enabled(componentID) {
		return 0
	}
	return Round(subject.BaseSeverity * policy.Weight(componentID))
}

func applyTypeCyclomaticResiduals(file *report.File, original report.File, policy Policy, groups map[string]float64) {
	typeComponent, ok := file.Components["cyclomatic_class_complexity"]
	if !ok || typeComponent.ScoringDefinition == nil {
		return
	}
	typeSubjects := original.Components["cyclomatic_class_complexity"].Subjects
	methodSubjects := original.Components["cyclomatic_method_complexity"].Subjects
	if len(typeSubjects) == 0 || len(methodSubjects) == 0 {
		return
	}
	methodGroups := map[int]map[string]bool{}
	unresolved := false
	typeOwners := make(map[string]int, len(typeSubjects))
	for typeIndex, subject := range typeSubjects {
		typeOwners[subject.Subject] = typeIndex
	}
	for methodIndex, method := range methodSubjects {
		bestType := -1
		if method.Owner != "" {
			if index, ok := typeOwners[method.Owner]; ok {
				bestType = index
			}
		}
		if bestType < 0 {
			unresolved = true
			continue
		}
		if methodGroups[bestType] == nil {
			methodGroups[bestType] = map[string]bool{}
		}
		routine := method.Routine
		if routine == "" {
			routine = projectionGroupKey("cyclomatic_method_complexity", method, methodIndex)
		}
		methodGroups[bestType][routine] = true
	}
	typeComponent.Contribution = 0
	for typeIndex, typeSubject := range typeSubjects {
		weightedType := projectedSubject(typeSubject, policy, "cyclomatic_class_complexity")
		charged := 0.0
		ownedGroups := make([]string, 0, len(methodGroups[typeIndex]))
		for group := range methodGroups[typeIndex] {
			ownedGroups = append(ownedGroups, group)
		}
		sort.Strings(ownedGroups)
		for _, group := range ownedGroups {
			charged += groups[group]
		}
		residual := math.Max(0, weightedType-charged)
		if typeIndex < len(typeComponent.Subjects) {
			typeComponent.Subjects[typeIndex].Contribution = Round(residual)
		} else {
			typeComponent.Subjects = append(typeComponent.Subjects, report.SubjectContribution{Subject: typeSubject.Subject, Value: typeSubject.Value, Contribution: Round(residual)})
		}
		if typeComponent.ScoringDefinition.Aggregation == "max" {
			typeComponent.Contribution = math.Max(typeComponent.Contribution, residual)
		} else {
			typeComponent.Contribution = Round(typeComponent.Contribution + residual)
		}
	}
	typeComponent.Contribution = Round(typeComponent.Contribution)
	file.Components["cyclomatic_class_complexity"] = typeComponent
	if unresolved {
		file.ScoringLimitations = appendUniqueString(file.ScoringLimitations, "type CYCLO routine association was incomplete; unassociated WMC retained")
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func addComponentContribution(component report.Component, value float64) report.Component {
	component.Contribution = Round(component.Contribution + value)
	return component
}
