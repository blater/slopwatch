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

// Parameter bindings are scoped to the operation. A file-wide identifier/type
// guess is not sufficient evidence that a caller manipulates this boundary.
func gradedParameterTypes(tokens []token, language string) map[string]string {
	result := map[string]string{}
	if language == "rust" || language == "typescript" {
		start := 0
		depth := 0
		for end := 0; end <= len(tokens); end++ {
			if end < len(tokens) {
				switch tokens[end].text {
				case "<", "(", "[":
					depth++
				case ">", ")", "]":
					depth--
				}
			}
			if end < len(tokens) && (tokens[end].text != "," || depth > 0) {
				continue
			}
			part := tokens[start:end]
			for i, t := range part {
				if t.text == ":" && i > 0 {
					result[part[i-1].text] = joinTokens(part[i+1:])
					break
				}
			}
			start = end + 1
		}
		return result
	}
	if language == "go" {
		names := []string{}
		for i := 0; i < len(tokens); {
			if tokens[i].text == "," {
				i++
				continue
			}
			if !isIdentifier(tokens[i].text) {
				break
			}
			names = append(names, tokens[i].text)
			i++
			if i >= len(tokens) {
				break
			}
			if tokens[i].text == "," {
				continue
			}
			start := i
			depth := 0
			for i < len(tokens) {
				if tokens[i].text == "[" {
					depth++
				}
				if tokens[i].text == "]" {
					depth--
				}
				if tokens[i].text == "," && depth == 0 {
					break
				}
				i++
			}
			typ := joinTokens(tokens[start:i])
			for _, name := range names {
				result[name] = typ
			}
			names = nil
		}
		return result
	}
	if language != "java" {
		return result
	}
	start := 0
	for end := 0; end <= len(tokens); end++ {
		if end < len(tokens) && tokens[end].text != "," {
			continue
		}
		part := tokens[start:end]
		name, typ := "", ""
		for _, t := range part {
			if isIdentifier(t.text) && t.text != "final" {
				if typ == "" {
					typ = t.text
				}
				name = t.text
			}
		}
		if name != "" && name != typ {
			result[name] = typ
		}
		start = end + 1
	}
	return result
}

// Build once, then share the bounded inbound representation index with every
// owner projection. Caller effects never become hidden duties of the callee.
func annotateCallerObligations(units []unit) {
	shadows := map[string]map[string]bool{}
	for _, u := range units {
		key := normalizeLanguage(u.file.Language, u.file.Path) + "#" + u.pkg
		if shadows[key] == nil {
			shadows[key] = map[string]bool{}
		}
		for _, name := range []string{"len", "panic", "Vec"} {
			if gradedPackageShadow(u, name) {
				shadows[key][name] = true
			}
		}

	}
	for i := range units {
		u := &units[i]
		u.shadowedBuiltins = shadows[normalizeLanguage(u.file.Language, u.file.Path)+"#"+u.pkg]
	}

	byKey := map[string][]*operation{}
	for _, u := range units {
		for _, op := range u.ops {
			indexOperation(byKey, op)
		}
	}
	types := map[string][]gradedCallerType{}
	packageTypes := map[string]map[string]bool{}
	for index, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
			continue
		}
		for i, t := range u.tokens {
			if t.text != "class" || i+1 >= len(u.tokens) {
				continue
			}
			owner := u.tokens[i+1].text
			decl := gradedFindSurfaceOwner(u.tokens, "java", owner)
			if decl == nil {
				continue
			}
			parent := ""
			for j := i + 2; j < decl.open; j++ {
				if u.tokens[j].text == "extends" && j+1 < decl.open {
					parent = u.tokens[j+1].text
				}
			}
			if packageTypes[u.pkg] == nil {
				packageTypes[u.pkg] = map[string]bool{}
			}
			packageTypes[u.pkg][owner] = true
			key := u.pkg + "#" + owner
			types[key] = append(types[key], gradedCallerType{index, owner, parent, decl.fields})
		}
	}
	for index := range units {
		units[index].packageTypes = packageTypes[units[index].pkg]
	}
	type fieldRef struct {
		owner string
		field gradedSurfaceField
		unit  int
	}
	resolve := func(pkg, typ, name string) (fieldRef, bool) {
		seen := map[string]bool{}
		for depth := 0; depth < maxCallDepth; depth++ {
			key := pkg + "#" + typ
			if seen[key] || len(types[key]) != 1 {
				return fieldRef{}, false
			}
			seen[key] = true
			d := types[key][0]
			if f, ok := d.fields[name]; ok {
				return fieldRef{d.owner, f, d.unit}, true
			}
			if d.parent == "" {
				return fieldRef{}, false
			}
			typ = d.parent
		}
		return fieldRef{}, false
	}
	type consumerRecord struct {
		constraint gradedConstraint
		refs       []fieldRef
		caller     *operation
		lifecycle  bool
	}
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
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
			continue
		}
		for _, op := range u.ops {
			copy := *op
			copy.body = gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)
			op = &copy

			for i, t := range op.body {
				if t.text == "return" && gradedUnconditional(op.body, i) {
					op.body = op.body[:statementEnd(op.body, i)]
					break
				}
			}
			bindings := map[string]string{}
			for name, f := range callerDeclaredFields(u, op.owner) {
				bindings[name] = f.typeName
			}
			for name, typ := range op.parameterTypes {
				bindings[name] = typ
			}
			// A local declaration shadows a field/parameter; ambiguous inferred or
			// assigned aliases are deliberately excluded from this bounded proof.
			for i := 1; i+1 < len(op.body); i++ {
				if isIdentifier(op.body[i].text) && isIdentifier(op.body[i-1].text) && (op.body[i+1].text == "=" || op.body[i+1].text == ";") {
					delete(bindings, op.body[i].text)
				}
			}
			for i, t := range op.body {
				if _, ok := bindings[t.text]; ok && i+1 < len(op.body) && op.body[i+1].text == "=" && (i == 0 || op.body[i-1].text != "." || i >= 2 && op.body[i-2].text == "this") {
					delete(bindings, t.text)
				}
			}
			for _, consumer := range gradedConsumerConstraints(op, u, units, byKey, map[string]map[string]bool{}, 0) {
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
					ref, ok := resolve(u.pkg, typ, parts[1])
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
			refs := map[string]map[string]fieldRef{}
			writes := map[string]map[string]map[string]bool{}
			boolWrites := map[string]bool{}
			calls := map[string]bool{}
			conditions := map[string]map[string]bool{}
			argumentRefs := map[string]bool{}
			predicateRefs := map[int]int{}
			for j, t := range op.body {
				if t.text == "if" && j+1 < len(op.body) && op.body[j+1].text == "(" {
					close := matching(op.body, j+1, "(", ")")
					if close > j {
						for k := j + 2; k < close; k++ {
							predicateRefs[k] = close + 1
						}
					}
				}
			}
			allCalls := callsIn(op.body)
			for _, c := range allCalls {
				for receiver := range bindings {
					if strings.HasPrefix(c.name, receiver+".") || gradedCallerCallRoot(op.body, c) == receiver {
						calls[receiver] = true
						for _, arg := range c.actuals {
							for dep := range gradedCallerDependencies(arg) {
								argumentRefs[dep] = true
							}
						}
					}
				}
			}
			epochs := map[string]int{}
			for i := 0; i < len(op.body); i++ {
				if i+1 < len(op.body) && gradedStorageWriteAt(op.body, i) {
					start := i
					for start >= 2 && op.body[start-1].text == "." {
						start -= 2
					}
					epochs[joinTokens(op.body[start:i+1])]++
				}
				if i < 2 {
					continue
				}
				if op.body[i-1].text != "." {
					continue
				}
				receiver, name := op.body[i-2].text, op.body[i].text
				typ := bindings[receiver]
				if typ == "" {
					continue
				}
				if i >= 4 && op.body[i-3].text == "." {
					if op.body[i-4].text != "this" {
						continue
					}
					typ = callerDeclaredFields(u, op.owner)[receiver].typeName
				}
				if typ == "" {
					continue
				}
				ref, ok := resolve(u.pkg, typ, name)
				if !ok || ref.owner == op.owner || !ref.field.mutable || (!ref.field.public && !ref.field.packageVisible) {
					continue
				}
				if refs[receiver] == nil {
					refs[receiver] = map[string]fieldRef{}
					writes[receiver] = map[string]map[string]bool{}
					conditions[receiver] = map[string]bool{}
				}
				refs[receiver][name] = ref
				if argumentRefs[receiver+"."+name] {
					conditions[receiver][name] = true
				}
				if i+1 < len(op.body) && op.body[i+1].text == "=" {
					controlled[itoa(ref.unit)+"#"+ref.owner+"#"+name] = true
					end := statementEnd(op.body, i+2)
					deps := gradedCallerDependencies(op.body[i+2 : end])
					versioned := map[string]bool{}
					for dep := range deps {
						root := strings.Split(dep, ".")[0]
						versioned[dep+"@"+itoa(epochs[dep])+"/"+itoa(epochs[root])] = true
					}
					deps = versioned
					writes[receiver][name] = deps
					if gradedExactBooleanAssignment(op.body, i, "true") || gradedExactBooleanAssignment(op.body, i, "false") {
						boolWrites[receiver] = true
						key := itoa(ref.unit) + "#" + ref.owner + "#" + name
						if transitions[key] == nil {
							transitions[key] = map[string]map[string]bool{}
						}
						state := op.body[i+2].text
						if transitions[key][state] == nil {
							transitions[key][state] = map[string]bool{}
						}
						transitions[key][state][op.id] = true
					}
				}
				// Only fields participating in an actual branch predicate establish
				// caller admission/selection work; a dead local read is not an obligation.
				if start, ok := predicateRefs[i]; ok && gradedCallerBranchEffect(op.body, start, receiver) {
					conditions[receiver][name] = true
				}
			}
			for receiver, fields := range refs {
				for name := range writes[receiver] {
					record(fields[name], op)
				}
			}

		}
	}
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

func gradedCallerCallRoot(body []token, c call) string {
	start := c.position
	for start >= 2 && body[start-1].text == "." {
		if body[start-2].text != ")" {
			start -= 2
			continue
		}
		open := -1
		for j := start - 3; j >= 0; j-- {
			if body[j].text == "(" && matching(body, j, "(", ")") == start-2 {
				open = j
				break
			}
		}
		if open < 1 {
			return ""
		}
		start = open - 1
	}
	if start >= 0 && start < len(body) {
		return body[start].text
	}
	return ""
}
