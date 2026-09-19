package sourceestimate

import (
	"sort"
	"strings"
)

type gradedCallerObligation struct {
	fields, evidence []string
	sequencing       bool
	constraints      []gradedConstraint
}
type gradedCallerType struct {
	unit          int
	owner, parent string
	fields        map[string]gradedSurfaceField
}

func annotateCallerObligations(units []unit) {
	byKey, types := gradedCallerPrepare(units)
	resolver := gradedCallerResolver{types: types}
	consumers := []consumerRecord{}
	controlled := map[string]bool{}
	transitions := map[string]map[string]map[string]bool{}
	connected := map[int]map[string]map[string]bool{}
	established := map[int]map[string]map[string]bool{}
	for index, u := range units {
		established[index] = map[string]map[string]bool{}
		for _, op := range u.ops {
			if established[index][op.owner] != nil {
				continue
			}
			established[index][op.owner] = map[string]bool{}
			for _, constraint := range gradedOwnerConstraints(u, op.owner, u.ops) {
				if constraint.protected {
					continue
				}
				for _, field := range constraint.fields {
					established[index][op.owner][field] = true
				}
			}
		}
	}
	witnesses := map[int]map[string]map[string]bool{}
	record := func(ref fieldRef, caller *operation) {
		if !established[ref.unit][ref.owner][ref.field.name] {
			return
		}
		if connected[ref.unit] == nil {
			connected[ref.unit] = map[string]map[string]bool{}
			witnesses[ref.unit] = map[string]map[string]bool{}
		}
		if connected[ref.unit][ref.owner] == nil {
			connected[ref.unit][ref.owner] = map[string]bool{}
			witnesses[ref.unit][ref.owner] = map[string]bool{}
		}
		connected[ref.unit][ref.owner][ref.field.name] = true
		witnesses[ref.unit][ref.owner][caller.id] = true
	}
	consumers = gradedCallerScanJava(units, byKey, resolver, consumers, controlled, transitions, record)
	accepted := map[int]map[string][]gradedConstraint{}
	seenConsumers := map[string]bool{}
	for _, consumer := range consumers {
		eligible := false
		for _, ref := range consumer.refs {
			key := itoa(ref.unit) + "#" + ref.owner + "#" + ref.field.name
			if !controlled[key] {
				continue
			}
			if consumer.lifecycle {
				phases := transitions[key]
				if len(phases["true"]) == 0 || len(phases["false"]) == 0 {
					continue
				}
			}
			eligible = true
		}
		if !eligible {
			continue
		}
		ref := consumer.refs[0]
		key := itoa(ref.unit) + "#" + consumer.constraint.id
		if seenConsumers[key] {
			continue
		}
		seenConsumers[key] = true
		if accepted[ref.unit] == nil {
			accepted[ref.unit] = map[string][]gradedConstraint{}
		}
		accepted[ref.unit][ref.owner] = append(accepted[ref.unit][ref.owner], consumer.constraint)
		for _, ref := range consumer.refs {
			if established[ref.unit][ref.owner] == nil {
				established[ref.unit][ref.owner] = map[string]bool{}
			}
			established[ref.unit][ref.owner][ref.field.name] = true
			if ref.field.mutable && controlled[itoa(ref.unit)+"#"+ref.owner+"#"+ref.field.name] {
				record(ref, consumer.caller)
			}
		}
	}

	for index, owners := range connected {
		units[index].callerObligations = map[string]gradedCallerObligation{}
		for owner, fields := range owners {
			entry := gradedCallerObligation{constraints: accepted[index][owner]}
			for field := range fields {
				entry.fields = append(entry.fields, field)
				phases := transitions[itoa(index)+"#"+owner+"#"+field]
				for opening := range phases["true"] {
					for closing := range phases["false"] {
						if opening != closing {
							entry.sequencing = true
						}
					}
				}
			}
			sort.Strings(entry.fields)
			for witness := range witnesses[index][owner] {
				entry.evidence = append(entry.evidence, "caller-connected-fields:"+strings.Join(entry.fields, ",")+":"+witness)
			}
			if entry.sequencing {
				entry.evidence = append(entry.evidence, "caller-shared-state-phases:"+owner)
			}
			sort.Strings(entry.evidence)
			units[index].callerObligations[owner] = entry
		}
	}
}
