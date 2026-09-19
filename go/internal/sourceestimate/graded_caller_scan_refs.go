package sourceestimate

import "strings"

// Parenthesis pairs and fluent roots depend only on the immutable body. The
// pair stack deliberately ignores other delimiter kinds, matching matching().
type gradedCallerBodyIndex struct{ pairs, roots []int }

func indexGradedCallerBody(body []token) gradedCallerBodyIndex {
	index := gradedCallerBodyIndex{pairs: make([]int, len(body)), roots: make([]int, len(body))}
	for i := range index.pairs {
		index.pairs[i] = -1
	}
	stack := []int{}
	for i, tok := range body {
		if tok.text == "(" {
			stack = append(stack, i)
		}
		if tok.text == ")" && len(stack) > 0 {
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			index.pairs[open] = i
			index.pairs[i] = open
		}
		root := i
		if i >= 2 && body[i-1].text == "." {
			if body[i-2].text != ")" {
				root = index.roots[i-2]
			} else if open := index.pairs[i-2]; open >= 1 {
				root = index.roots[open-1]
			} else {
				root = -1
			}
		}
		index.roots[i] = root
	}
	return index
}

func (index gradedCallerBodyIndex) callRoot(body []token, c call) string {
	if c.position < 0 || c.position >= len(body) {
		return ""
	}
	root := index.roots[c.position]
	if root < 0 {
		return ""
	}
	return body[root].text
}

func gradedCallerCallRoot(body []token, c call) string {
	return indexGradedCallerBody(body).callRoot(body, c)
}

func gradedCallerPredicateRefs(body []token) map[int]int {
	return gradedCallerPredicateRefsIndexed(body, indexGradedCallerBody(body))
}

func gradedCallerPredicateRefsIndexed(body []token, index gradedCallerBodyIndex) map[int]int {
	refs := map[int]int{}
	ends := []int{}
	for i := range body {
		for len(ends) > 0 && ends[len(ends)-1] <= i {
			ends = ends[:len(ends)-1]
		}
		if i >= 2 && body[i-2].text == "if" && body[i-1].text == "(" && index.pairs[i-1] > i {
			ends = append(ends, index.pairs[i-1])
		}
		if len(ends) > 0 {
			refs[i] = ends[len(ends)-1] + 1
		}
	}
	return refs
}

func gradedCallerArgumentRefs(body []token, bindings map[string]string) map[string]bool {
	return gradedCallerArgumentRefsIndexed(body, bindings, indexGradedCallerBody(body))
}

func gradedCallerArgumentRefsIndexed(body []token, bindings map[string]string, index gradedCallerBodyIndex) map[string]bool {
	refs := map[string]bool{}
	for _, c := range callsIn(body) {
		bound := false
		// Preserve all prefix spellings, including dotted keys and empty-valued
		// bindings: eligibility depends on membership, not the resolved type.
		for i := 0; i < len(c.name); i++ {
			if c.name[i] == '.' {
				if _, ok := bindings[c.name[:i]]; ok {
					bound = true
					break
				}
			}
		}
		if !bound {
			_, bound = bindings[index.callRoot(body, c)]
		}
		if !bound {
			continue
		}
		for _, arg := range c.actuals {
			for dep := range gradedCallerDependencies(arg) {
				refs[dep] = true
			}
		}
	}
	return refs
}

func gradedCallerScanRefs(u unit, op *operation, bindings map[string]string, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool) (map[string]map[string]fieldRef, map[string]map[string]map[string]bool, map[string]map[string]bool) {
	refs := map[string]map[string]fieldRef{}
	writes := map[string]map[string]map[string]bool{}
	conditions := map[string]map[string]bool{}
	bodyIndex := indexGradedCallerBody(op.body)
	argumentRefs := gradedCallerArgumentRefsIndexed(op.body, bindings, bodyIndex)
	predicateRefs := gradedCallerPredicateRefsIndexed(op.body, bodyIndex)
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
