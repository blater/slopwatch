package sourceestimate

import (
	"sort"
	"strings"
)

func gradedCallerDependencies(body []token) map[string]bool {
	result := map[string]bool{}
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if !isIdentifier(t) || t == "true" || t == "false" || t == "null" || t == "new" {
			continue
		}
		if i > 0 && body[i-1].text == "." {
			continue
		}
		end := i + 1
		for end+1 < len(body) && body[end].text == "." && isIdentifier(body[end+1].text) {
			end += 2
		}
		if end < len(body) && body[end].text == "(" {
			i = end - 1
			continue
		}
		result[joinTokens(body[i:end])] = true
		i = end - 1
	}
	return result
}

func gradedCallerBranchEffect(body []token, start int, receiver string) bool {
	if start >= len(body) {
		return false
	}
	end := statementEnd(body, start)
	if body[start].text == "{" {
		end = matching(body, start, "{", "}")
	}
	if end < start {
		return false
	}
	segment := body[start:end]
	for _, t := range segment {
		if t.text == "return" || t.text == "throw" {
			return true
		}
	}
	// A predicate must govern an actual operation, not an empty/dead branch.
	for _, c := range callsIn(segment) {
		if strings.HasPrefix(c.name, receiver+".") || !strings.Contains(c.name, ".") {
			return true
		}
		produced := gradedCallAssignedName(segment, c)
		observed := false
		if produced != "" {
			for j := end + 1; j < len(body); j++ {
				if body[j].text == produced && !gradedStorageWriteAt(body, j) {
					observed = true
					break
				}
			}
		}
		if observed {
			for _, arg := range c.actuals {
				for dep := range gradedCallerDependencies(arg) {
					if strings.HasPrefix(dep, receiver+".") {
						return true
					}
				}
			}
		}
	}
	return false
}

func gradedCallerCollectKnownConsumers(u unit, op *operation, units []unit, resolver gradedCallerResolver, bindings map[string]string, constraints []gradedConsumerConstraint) []consumerRecord {
	consumers := []consumerRecord{}
	for _, consumer := range constraints {
		grouped := map[string][]fieldRef{}
		for dep := range consumer.fields {
			parts := strings.Split(dep, ".")
			if len(parts) != 2 {
				continue
			}
			typ := bindings[parts[0]]
			if typ == "" {
				continue
			}
			ref, ok := resolver.resolve(u.pkg, typ, parts[1])
			if !ok || ref.owner == op.owner {
				continue
			}
			key := itoa(ref.unit) + "#" + ref.owner
			grouped[key] = append(grouped[key], ref)
		}
		for _, refs := range grouped {
			names := []string{}
			for _, ref := range refs {
				names = append(names, ref.field.name)
			}
			sort.Strings(names)
			names = uniqueConstraintStrings(names)
			if len(names) < 2 && !consumer.lifecycle {
				continue
			}
			kind := "resolved-callee-precondition"
			if consumer.lifecycle {
				kind = "phase-admission"
			}
			source := units[consumer.op.file].file
			offset := 0
			if len(consumer.op.body) > 0 {
				offset = consumer.op.body[0].offset
			}
			consumers = append(consumers, consumerRecord{constraint: gradedConstraint{id: refs[0].owner + "#" + kind + "#" + strings.Join(names, ","), kind: kind, fields: names, operation: consumer.op.id, offset: offset, source: &source}, refs: refs, caller: op, lifecycle: consumer.lifecycle})
		}
	}
	return consumers
}
