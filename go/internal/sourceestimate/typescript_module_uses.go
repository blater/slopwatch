package sourceestimate

// A scan owns the lexical alias stack and bounded traversal state of one call.
// Child scans share only the recursion guard and visit budget.
type typeScriptModuleScan struct {
	op           *operation
	storage      map[string]string
	units        []unit
	index        *operationLookup
	seen         map[string]bool
	bindings     map[string]string
	visits       *int
	depth        int
	body         []token
	aliases      map[string]string
	scopes       []map[string]string
	declarations map[int]gradedOutputDeclaration
	result       map[string]typeScriptModuleUse
}

func typeScriptModuleUses(op *operation, storage map[string]string, units []unit, index *operationLookup, seen map[string]bool, bindings map[string]string, visits *int, depth int) map[string]typeScriptModuleUse {
	result := map[string]typeScriptModuleUse{}
	if depth >= maxCallDepth || seen[op.id] || *visits >= maxCallsPerRoot {
		return result
	}
	seen[op.id] = true
	*visits++
	defer delete(seen, op.id)
	var body []token
	if op.language == "typescript" {
		body = normalizedEagerBody(op)
	} else {
		body = gradedEagerBody(normalizedPrunedBody(op), "typescript")
	}
	for i, t := range body {
		if t.text == "return" && operationBodyUnconditional(op, body, i) {
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

	s := typeScriptModuleScan{op: op, storage: storage, units: units, index: index, seen: seen, bindings: bindings, visits: visits, depth: depth,
		body: body, aliases: aliases, scopes: []map[string]string{{}}, declarations: gradedOutputDeclarations(body, "typescript"), result: result}
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	for i, t := range body {
		s.scopeToken(t)
		if s.declare(i, t) {
			continue
		}
		s.recordUse(i, t)
		if c, ok := calls[i]; ok {
			s.followCall(c)
		}
	}
	return result
}

func (s *typeScriptModuleScan) scopeToken(t token) {
	if t.text == "{" {
		s.scopes = append(s.scopes, map[string]string{})
	}
	if t.text == "}" && len(s.scopes) > 1 {
		for name, old := range s.scopes[len(s.scopes)-1] {
			if old == "" {
				delete(s.aliases, name)
			} else {
				s.aliases[name] = old
			}
		}
		s.scopes = s.scopes[:len(s.scopes)-1]
	}
}

func (s *typeScriptModuleScan) declare(i int, t token) bool {
	if declaration, ok := s.declarations[i]; ok {
		if declaration.block {
			scope := s.scopes[len(s.scopes)-1]
			if _, exists := scope[t.text]; !exists {
				scope[t.text] = s.aliases[t.text]
			}
		}
		root := ""
		if len(declaration.initializer) == 1 && operationBodyUnconditional(s.op, s.body, i) {
			root = s.aliases[declaration.initializer[0].text]
		}
		delete(s.aliases, t.text)
		if root != "" {
			s.aliases[t.text] = root
		}
		return true
	}
	return false
}

func (s *typeScriptModuleScan) recordUse(i int, t token) {
	root := s.aliases[t.text]
	if root == "" || i > 0 && s.body[i-1].text == "." {
		return
	}
	use := s.result[root]
	s.recordWrite(i, t.text, root, &use)
	if typeScriptCollectionWrite(s.body, i, s.storage[root]) {
		use.write = true
	}
	if typeScriptReturnedReference(s.body, i) {
		use.read = true
	}
	s.result[root] = use
}

func (s *typeScriptModuleScan) recordWrite(i int, name, root string, use *typeScriptModuleUse) {
	write := gradedStorageWriteAt(s.body, i) || i > 0 && (s.body[i-1].text == "++" || s.body[i-1].text == "--")
	if !write {
		return
	}
	// Assigning an alias changes only the local reference. Only writes to the
	// actual module binding contribute a storage effect.
	if name != root || s.bindings[name] != "" {
		if i+1 < len(s.body) && s.body[i+1].text == "=" {
			delete(s.aliases, name)
		}
		return
	}
	use.write = true
	if s.storage[root] != "scalar" {
		delete(s.aliases, name)
		return
	}
	state, computed := typeScriptScalarEffect(s.body, i)
	use.scalarState = use.scalarState || state
	use.scalarComputed = use.scalarComputed || computed
}

func typeScriptCollectionWrite(body []token, i int, kind string) bool {
	if i+3 >= len(body) || body[i+1].text != "." || body[i+3].text != "(" {
		return false
	}
	switch kind {
	case "Set":
		return body[i+2].text == "add" || body[i+2].text == "delete" || body[i+2].text == "clear"
	case "Map":
		return body[i+2].text == "set" || body[i+2].text == "delete" || body[i+2].text == "clear"
	}
	return false
}

func (s *typeScriptModuleScan) followCall(c call) {
	matches := resolveCall(s.op, c, s.units, s.index)
	if matches.count() != 1 || matches.unique().file != s.op.file || matches.unique().owner != "" || matches.unique().exposed {
		return
	}
	childBindings := map[string]string{}
	for j, p := range matches.unique().paramNames {
		if j >= len(c.actuals) || len(c.actuals[j]) != 1 {
			continue
		}
		if root := s.aliases[c.actuals[j][0].text]; root != "" {
			childBindings[p] = root
		}
	}
	for field, use := range typeScriptModuleUses(matches.unique(), s.storage, s.units, s.index, s.seen, childBindings, s.visits, s.depth+1) {
		prior := s.result[field]
		prior.read = prior.read || use.read && typeScriptCallResultObserved(s.body, c)
		prior.write = prior.write || use.write
		prior.scalarState = prior.scalarState || use.scalarState
		prior.scalarComputed = prior.scalarComputed || use.scalarComputed
		s.result[field] = prior
	}
}
