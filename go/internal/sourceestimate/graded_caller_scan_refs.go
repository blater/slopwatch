package sourceestimate

import "strings"

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

func gradedCallerPredicateRefs(body []token) map[int]int {
	refs := map[int]int{}
	for j, t := range body {
		if t.text != "if" || j+1 >= len(body) || body[j+1].text != "(" {
			continue
		}
		close := matching(body, j+1, "(", ")")
		if close <= j {
			continue
		}
		for k := j + 2; k < close; k++ {
			refs[k] = close + 1
		}
	}
	return refs
}

func gradedCallerArgumentRefs(body []token, bindings map[string]string) map[string]bool {
	refs := map[string]bool{}
	for _, c := range callsIn(body) {
		for receiver := range bindings {
			if !strings.HasPrefix(c.name, receiver+".") && gradedCallerCallRoot(body, c) != receiver {
				continue
			}
			for _, arg := range c.actuals {
				for dep := range gradedCallerDependencies(arg) {
					refs[dep] = true
				}
			}
		}
	}
	return refs
}

func gradedCallerScanRefs(u unit, op *operation, bindings map[string]string, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool) (map[string]map[string]fieldRef, map[string]map[string]map[string]bool, map[string]map[string]bool) {
	refs := map[string]map[string]fieldRef{}
	writes := map[string]map[string]map[string]bool{}
	conditions := map[string]map[string]bool{}
	argumentRefs := gradedCallerArgumentRefs(op.body, bindings)
	predicateRefs := gradedCallerPredicateRefs(op.body)
	epochs := map[string]int{}
	for i := 0; i < len(op.body); i++ {
		if i+1 < len(op.body) && gradedStorageWriteAt(op.body, i) {
			start := i
			for start >= 2 && op.body[start-1].text == "." {
				start -= 2
			}
			epochs[joinTokens(op.body[start:i+1])]++
		}
		if i < 2 || op.body[i-1].text != "." {
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
		ref, ok := resolver.resolve(u.pkg, typ, name)
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
			writes[receiver][name] = gradedCallerDependencyVersions(op.body[i+2:end], epochs)
			if gradedExactBooleanAssignment(op.body, i, "true") || gradedExactBooleanAssignment(op.body, i, "false") {
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
	return refs, writes, conditions
}

// Dependency identities retain both the field and receiver write epochs.
func gradedCallerDependencyVersions(expression []token, epochs map[string]int) map[string]bool {
	versioned := map[string]bool{}
	for dep := range gradedCallerDependencies(expression) {
		root := strings.Split(dep, ".")[0]
		versioned[dep+"@"+itoa(epochs[dep])+"/"+itoa(epochs[root])] = true
	}
	return versioned
}
