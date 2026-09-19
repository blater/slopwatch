package sourceestimate

import (
	"sort"
	"strings"
)

// Project caller-side uses through unambiguous parameter/storage type bindings.
// This reuses the same constraint recognizer; writes alone never create one.
// Imports, reassigned receivers and ambiguous type names remain unresolved.
func annotateConstraintWitnesses(units []unit) {
	type target struct {
		index int
		owner string
	}
	registry := map[string][]target{}
	for i, u := range units {
		language := normalizeLanguage(u.file.Language, u.file.Path)
		for _, owner := range gradedDeclaredConstraintOwners(u) {
			key := language + "#" + u.pkg + "#" + owner
			registry[key] = append(registry[key], target{i, owner})
		}

	}
	byKey := map[string][]*operation{}
	for _, u := range units {
		for _, op := range u.ops {
			indexOperation(byKey, op)
		}
	}
	type phaseWitness struct {
		target target
		record gradedConstraint
	}
	phases := []phaseWitness{}
	writes := map[string]map[string]map[string]bool{}

	for _, u := range units {
		for _, original := range u.ops {
			eager := *original
			eager.body = gradedEagerBody(pruneDeadFalseBranches(original.body), original.language)
			op := &eager
			bindings := map[string]string{}
			for name, typ := range op.parameterTypes {
				bindings[name] = typ
			}
			for name, field := range callerDeclaredFields(u, op.owner) {
				if bindings[name] == "" {
					bindings[name] = field.typeName
				}
			}
			for receiver, typ := range bindings {
				typ = strings.TrimSpace(strings.TrimLeft(typ, "*&"))
				typ = strings.TrimPrefix(typ, "mut")
				candidates := registry[op.language+"#"+u.pkg+"#"+typ]
				if len(candidates) != 1 {
					continue
				}
				target := candidates[0]
				if target.index == op.file && target.owner == op.owner {
					continue
				}
				reassigned := false
				for i, tok := range op.body {
					if tok.text == receiver && i+1 < len(op.body) && (op.body[i+1].text == "=" || op.body[i+1].text == ":=") {
						reassigned = true
					}
				}
				if reassigned {
					continue
				}
				if op.language != "java" {
					for i := 2; i+2 < len(op.body); i++ {
						if op.body[i-2].text != receiver || op.body[i-1].text != "." || op.body[i+1].text != "=" || (!gradedExactBooleanAssignment(op.body, i, "true") && !gradedExactBooleanAssignment(op.body, i, "false")) {
							continue
						}
						key := itoa(target.index) + "#" + target.owner + "#" + op.body[i].text
						if writes[key] == nil {
							writes[key] = map[string]map[string]bool{}
						}
						value := op.body[i+2].text
						if writes[key][value] == nil {
							writes[key][value] = map[string]bool{}
						}
						writes[key][value][op.id] = true
					}
					for _, consumer := range gradedConsumerConstraints(op, u, units, byKey, map[string]map[string]bool{}, 0) {
						names := []string{}
						for field := range consumer.fields {
							prefix := receiver + "."
							if strings.HasPrefix(field, prefix) {
								name := strings.TrimPrefix(field, prefix)
								if _, ok := callerDeclaredFields(units[target.index], target.owner)[name]; ok {
									names = append(names, name)
								}
							}
						}
						sort.Strings(names)
						names = uniqueConstraintStrings(names)
						if consumer.lifecycle && len(names) != 1 || !consumer.lifecycle && len(names) < 2 {
							continue
						}
						source := units[consumer.op.file].file
						offset := 0
						if len(consumer.op.body) > 0 {
							offset = consumer.op.body[0].offset
						}
						kind := "resolved-callee-precondition"
						if consumer.lifecycle {
							kind = "phase-admission"
						}
						record := gradedConstraint{id: target.owner + "#" + kind + "#" + strings.Join(names, ","), kind: kind, fields: names, operation: consumer.op.id, offset: offset, source: &source}
						phases = append(phases, phaseWitness{target: target, record: record})
					}

				}
				copy := *op
				copy.owner = target.owner
				copy.receiverName = receiver
				copy.constraintReceiver = receiver
				records := gradedOwnerConstraints(units[target.index], target.owner, []*operation{&copy})
				if len(records) == 0 {
					continue
				}
				if units[target.index].callerObligations == nil {
					units[target.index].callerObligations = map[string]gradedCallerObligation{}
				}
				entry := units[target.index].callerObligations[target.owner]
				declared := callerDeclaredFields(units[target.index], target.owner)
				for _, record := range records {
					source := u.file
					record.source = &source
					// Every participant must refer to this typed receiver in the witness.
					qualified := true
					for _, field := range record.fields {
						found := false
						for i := 2; i < len(op.body); i++ {
							found = found || op.body[i].text == field && op.body[i-1].text == "." && op.body[i-2].text == receiver
						}
						qualified = qualified && found
					}
					if !qualified {
						continue
					}
					for _, field := range record.fields {
						f := declared[field]
						if f.mutable && (f.public || f.packageVisible) {
							entry.fields = append(entry.fields, field)
						}
					}
					entry.constraints = append(entry.constraints, record)
					entry.evidence = append(entry.evidence, record.evidence(u))
				}
				sort.Strings(entry.fields)
				entry.fields = uniqueConstraintStrings(entry.fields)
				units[target.index].callerObligations[target.owner] = entry
			}
		}
	}
	seen := map[string]bool{}
	for _, phase := range phases {
		target, record := phase.target, phase.record
		key := itoa(target.index) + "#" + target.owner + "#" + record.fields[0]
		states := writes[key]
		distinct := false
		for open := range states["true"] {
			for close := range states["false"] {
				distinct = distinct || open != close
			}
		}
		if record.kind == "phase-admission" && !distinct || seen[key+record.id] {
			continue
		}
		seen[key+record.id] = true
		if units[target.index].callerObligations == nil {
			units[target.index].callerObligations = map[string]gradedCallerObligation{}
		}
		entry := units[target.index].callerObligations[target.owner]
		entry.sequencing = entry.sequencing || record.kind == "phase-admission"
		entry.fields = append(entry.fields, record.fields...)
		entry.constraints = append(entry.constraints, record)
		units[target.index].callerObligations[target.owner] = entry
	}

}
func uniqueConstraintStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

// Storage declarations exist independently of methods. An unrelated method
// cannot create (or erase) the caller's resolved storage identity.
func gradedDeclaredConstraintOwners(u unit) []string {
	language := normalizeLanguage(u.file.Language, u.file.Path)
	seen := map[string]bool{}
	result := []string{}
	for i, t := range u.tokens {
		if i+1 >= len(u.tokens) {
			continue
		}
		if t.text != "class" && t.text != "struct" && t.text != "type" {
			continue
		}
		name := u.tokens[i+1].text
		if seen[name] || gradedFindSurfaceOwner(u.tokens, language, name) == nil {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}
