package sourceestimate

import (
	"sort"
	"strings"
)

// Module state connects public routes only when their eager effects refer to
// the same declared storage. Mere co-location or matching method names do not
// turn a file into an abstraction.
type typeScriptModuleUse struct{ read, write, scalarState, scalarComputed bool }

func typeScriptModuleGroups(u unit, units []unit, index map[string][]*operation) map[string]string {
	storage := typeScriptModuleStorage(u.tokens)
	if len(storage) == 0 {
		return nil
	}
	roots := []*operation{}
	uses := map[string]map[string]typeScriptModuleUse{}
	for _, op := range u.ops {
		if op.owner != "" || !op.exposed {
			continue
		}
		roots = append(roots, op)
		visits := 0
		uses[op.id] = typeScriptModuleUses(op, storage, units, index, map[string]bool{}, nil, &visits, 0)
	}
	parent := make([]int, len(roots))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	users := map[string][]int{}
	writers := map[string]bool{}
	for i, root := range roots {
		for field, use := range uses[root.id] {
			if use.read || use.write {
				users[field] = append(users[field], i)
			}
			writers[field] = writers[field] || use.write
		}
	}
	for field, indices := range users {
		if !writers[field] || len(indices) < 2 {
			continue
		}
		first := indices[0]
		for _, i := range indices[1:] {
			parent[find(i)] = find(first)
		}
	}
	members := map[int][]int{}
	for i := range roots {
		p := find(i)
		members[p] = append(members[p], i)
	}
	groupFields := map[int][]string{}
	for field, indices := range users {
		if writers[field] && len(indices) > 1 {
			p := find(indices[0])
			groupFields[p] = append(groupFields[p], field)
		}
	}
	result := map[string]string{}
	for p, group := range members {
		if len(group) < 2 {
			continue
		}
		fields := groupFields[p]
		sort.Strings(fields)
		key := "module-state:" + strings.Join(fields, ",")
		for _, i := range group {
			result[roots[i].id] = key
		}
	}
	return result
}

func typeScriptModuleStorage(body []token) map[string]string {
	result := map[string]string{}
	shadow := map[string]bool{}
	depth := 0
	for i, t := range body {
		if t.text == "{" {
			depth++
		}
		if t.text == "}" {
			depth--
		}
		if depth != 0 {
			continue
		}
		if (t.text == "class" || t.text == "interface" || t.text == "type" || t.text == "function") && i+1 < len(body) {
			shadow[body[i+1].text] = true
		}
		if t.text == "import" {
			end := statementEnd(body, i)
			for _, v := range body[i:end] {
				if v.text == "Set" || v.text == "Map" {
					shadow[v.text] = true
				}
			}
		}
		if (t.text == "const" || t.text == "let" || t.text == "var") && i+1 < len(body) {
			shadow[body[i+1].text] = true
		}
	}
	depth = 0
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if t == "{" {
			depth++
		}
		if t == "}" {
			depth--
		}
		if depth != 0 || (t != "let" && t != "var" && t != "const") || i+2 >= len(body) || !isIdentifier(body[i+1].text) {
			continue
		}
		end := statementEnd(body, i)
		assign := i + 2
		for assign < end && body[assign].text != "=" {
			assign++
		}
		if assign+1 >= end {
			continue
		}
		init := body[assign+1 : end]
		if len(init) == 1 && t != "const" && (init[0].kind == "number" || init[0].kind == "string" || init[0].text == "true" || init[0].text == "false") {
			result[body[i+1].text] = "scalar"
		}
		if len(init) >= 4 && init[0].text == "new" && (init[1].text == "Set" || init[1].text == "Map") && !shadow[init[1].text] {
			open := 2
			if init[open].text == "<" {
				close := matching(init, open, "<", ">")
				if close < 0 {
					continue
				}
				open = close + 1
			}
			if open+1 < len(init) && init[open].text == "(" && matching(init, open, "(", ")") == len(init)-1 {
				result[body[i+1].text] = init[1].text
			}
		}
	}
	return result
}

func typeScriptModuleUses(op *operation, storage map[string]string, units []unit, index map[string][]*operation, seen map[string]bool, bindings map[string]string, visits *int, depth int) map[string]typeScriptModuleUse {
	result := map[string]typeScriptModuleUse{}
	if depth >= maxCallDepth || seen[op.id] || *visits >= maxCallsPerRoot {
		return result
	}
	seen[op.id] = true
	*visits++
	defer delete(seen, op.id)
	body := gradedEagerBody(pruneDeadFalseBranches(op.body), "typescript")
	for i, t := range body {
		if t.text == "return" && gradedUnconditional(body, i) {
			body = body[:statementEnd(body, i)]
			break
		}
	}
	aliases := map[string]string{}
	for name := range storage {
		aliases[name] = name
	}
	for _, p := range op.paramNames {
		delete(aliases, p)
		if root := bindings[p]; root != "" {
			aliases[p] = root
		}
	}
	declarations := gradedOutputDeclarations(body, "typescript")
	scopes := []map[string]string{{}}
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	for i, t := range body {
		if t.text == "{" {
			scopes = append(scopes, map[string]string{})
		}
		if t.text == "}" && len(scopes) > 1 {
			for name, old := range scopes[len(scopes)-1] {
				if old == "" {
					delete(aliases, name)
				} else {
					aliases[name] = old
				}
			}
			scopes = scopes[:len(scopes)-1]
		}
		if declaration, ok := declarations[i]; ok {
			if declaration.block {
				scope := scopes[len(scopes)-1]
				if _, exists := scope[t.text]; !exists {
					scope[t.text] = aliases[t.text]
				}
			}
			root := ""
			if len(declaration.initializer) == 1 && gradedUnconditional(body, i) {
				root = aliases[declaration.initializer[0].text]
			}
			delete(aliases, t.text)
			if root != "" {
				aliases[t.text] = root
			}
			continue
		}
		root := aliases[t.text]
		if root != "" && (i == 0 || body[i-1].text != ".") {
			use := result[root]
			if gradedStorageWriteAt(body, i) || i > 0 && (body[i-1].text == "++" || body[i-1].text == "--") {
				// Writes to the actual module binding remain state effects; assigning an
				// alias only changes the local reference.
				if t.text == root && bindings[t.text] == "" {
					use.write = true
					if storage[root] == "scalar" {
						state, computed := typeScriptScalarEffect(body, i)
						use.scalarState = use.scalarState || state
						use.scalarComputed = use.scalarComputed || computed
					}
					if storage[root] != "scalar" {
						delete(aliases, t.text)
					}
				} else if i+1 < len(body) && body[i+1].text == "=" {
					delete(aliases, t.text)
				}
			}
			if i+3 < len(body) && body[i+1].text == "." && body[i+3].text == "(" {
				method := body[i+2].text
				kind := storage[root]
				if kind == "Set" && (method == "add" || method == "delete" || method == "clear") || kind == "Map" && (method == "set" || method == "delete" || method == "clear") {
					use.write = true
				}
			}
			if typeScriptReturnedReference(body, i) {
				use.read = true
			}
			result[root] = use
		}
		if c, ok := calls[i]; ok {
			matches := resolveCall(op, c, units, index)
			if len(matches) == 1 && matches[0].file == op.file && matches[0].owner == "" && !matches[0].exposed {
				childBindings := map[string]string{}
				for j, p := range matches[0].paramNames {
					if j < len(c.actuals) && len(c.actuals[j]) == 1 {
						if root := aliases[c.actuals[j][0].text]; root != "" {
							childBindings[p] = root
						}
					}
				}
				for field, use := range typeScriptModuleUses(matches[0], storage, units, index, seen, childBindings, visits, depth+1) {
					prior := result[field]
					prior.read = prior.read || use.read && typeScriptCallResultObserved(body, c)
					prior.write = prior.write || use.write
					prior.scalarState = prior.scalarState || use.scalarState
					prior.scalarComputed = prior.scalarComputed || use.scalarComputed
					result[field] = prior
				}
			}
		}
	}
	return result
}

func typeScriptReturnedReference(body []token, index int) bool {
	for i := index - 1; i >= 0; i-- {
		switch body[i].text {
		case ";", "{", "}":
			return false
		case "return":
			return true
		}
	}
	return false
}

// A helper's read contributes only when its result escapes this operation.
// Discarded read calls do not connect otherwise unrelated public routes.
func typeScriptCallResultObserved(body []token, c call) bool {
	start := c.position
	for start >= 2 && body[start-1].text == "." {
		start -= 2
	}
	if start > 0 && body[start-1].text == "return" {
		return true
	}
	if start < 2 || body[start-1].text != "=" || !isIdentifier(body[start-2].text) {
		return false
	}
	name := body[start-2].text
	declarations := gradedOutputDeclarations(body, "typescript")
	binding := typeScriptLocalBinding(body, declarations, name, start-2)
	if c.position+1 >= len(body) || body[c.position+1].text != "(" {
		return false
	}
	close := matching(body, c.position+1, "(", ")")
	if close < 0 {
		return false
	}
	for i := close + 1; i < len(body); i++ {
		if body[i].text == name && (i == 0 || body[i-1].text != ".") {
			if typeScriptLocalBinding(body, declarations, name, i) != binding {
				continue
			}
			if gradedStorageWriteAt(body, i) {
				return false
			}
			if typeScriptReturnedReference(body, i) {
				return true
			}
		}
	}
	return false
}

// Module-private storage retains the same duty as equivalent class-private
// storage. Grouping must not erase a counter update simply because it is lexical.
func typeScriptModuleScalarEffects(u unit, roots []*operation, units []unit, index map[string][]*operation) (state, computed bool) {
	if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" {
		return false, false
	}
	storage := typeScriptModuleStorage(u.tokens)
	for _, root := range roots {
		if root.owner != "" {
			continue
		}
		visits := 0
		for _, effect := range typeScriptModuleUses(root, storage, units, index, map[string]bool{}, nil, &visits, 0) {
			state = state || effect.scalarState
			computed = computed || effect.scalarComputed
		}
	}
	return
}
func typeScriptScalarEffect(body []token, index int) (state, computed bool) {
	if index > 0 && (body[index-1].text == "++" || body[index-1].text == "--") {
		return true, false
	}
	if index+1 >= len(body) {
		return false, false
	}
	operator := body[index+1].text
	if operator == "++" || operator == "--" {
		return true, false
	}
	if index+2 >= len(body) {
		return false, false
	}
	rhs := body[index+2 : statementEnd(body, index+2)]
	if (operator == "+=" || operator == "-=") && len(rhs) == 1 && rhs[0].text == "1" {
		return true, false
	}
	if operator == "=" {
		if len(rhs) == 3 && rhs[0].text == body[index].text && (rhs[1].text == "+" || rhs[1].text == "-") && rhs[2].text == "1" {
			return true, false
		}
		return false, hasTransform(rhs)
	}
	if operator == "+=" || operator == "-=" || operator == "*=" || operator == "/=" {
		return false, compoundUpdateChanges(rhs, operator)
	}
	return false, false
}

func typeScriptLocalBinding(body []token, declarations map[int]gradedOutputDeclaration, name string, before int) int {
	binding := -1
	for position, declaration := range declarations {
		if position <= before && position > binding && body[position].text == name && (!declaration.block || gradedScopeContains(body, position, before)) {
			binding = position
		}
	}
	return binding
}
