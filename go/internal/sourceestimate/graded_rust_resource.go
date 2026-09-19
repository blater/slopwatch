package sourceestimate

import "strings"

type gradedRustFlow struct {
	fields   map[string]bool
	identity string
	kind     string
	inner    string
}

// Exact imported pointer operations expose observable destruction/relocation.
// Their operands must be connected to declared self storage. Drop is an implicit
// language entry point; an identically named ordinary method carries no credit.
func gradedRustResourceEffects(u unit, roots []*operation) (resource, relocation bool) {
	if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
		return
	}
	owners := map[string]bool{}
	for _, op := range roots {
		owners[op.owner] = true
	}
	for owner := range owners {
		if owner == "" {
			continue
		}
		fields := gradedRustPointerFields(u, owner)
		for _, op := range roots {
			if op.owner == owner {
				_, moved := gradedRustPointerEffects(gradedRustExpandHelpers(op.body, u, owner, 0, nil), u.tokens, fields, "self")
				relocation = relocation || moved
			}
		}
		for _, fn := range rustFunctions(u.tokens) {
			if fn.owner != owner || fn.name != "drop" || fn.impl == nil || !gradedRustStandardDrop(u.tokens, *fn.impl) {
				continue
			}
			body := u.tokens[fn.bodyStart+1 : fn.bodyEnd]
			destroyed, moved := gradedRustPointerEffects(gradedRustExpandHelpers(body, u, owner, 0, nil), u.tokens, fields, "self")
			resource = resource || destroyed
			relocation = relocation || moved
		}
	}
	return
}

func gradedRustStandardDrop(tokens []token, impl rustImplRange) bool {
	if impl.trait != "Drop" {
		return false
	}
	for i, t := range tokens {
		if t.text == "trait" && i+1 < len(tokens) && tokens[i+1].text == "Drop" {
			return false
		}
		if t.text == "use" {
			end := i + 1
			for end < len(tokens) && tokens[end].text != ";" {
				end++
			}
			path := joinTokens(tokens[i+1 : end])
			hasDrop := false
			for _, part := range tokens[i+1 : end] {
				hasDrop = hasDrop || part.text == "Drop"
			}
			if hasDrop && !strings.HasPrefix(path, "core::ops::") && !strings.HasPrefix(path, "std::ops::") {
				return false
			}
		}
	}
	start := impl.start + 1
	if start < len(tokens) && tokens[start].text == "<" {
		end := rustGenericEnd(tokens, start)
		if end < 0 {
			return false
		}
		start = end + 1
	}
	end := start
	for end < len(tokens) && tokens[end].text != "for" && tokens[end].text != "{" {
		end++
	}
	path := joinTokens(tokens[start:end])
	return path == "Drop" || path == "core::ops::Drop" || path == "std::ops::Drop"
}

func gradedRustPointerContracts(tokens []token) map[string]bool {
	for i, t := range tokens {
		if t.text == "mod" && i+1 < len(tokens) && (tokens[i+1].text == "core" || tokens[i+1].text == "std") {
			return nil
		}
	}
	contracts := map[string]bool{"core.ptr.copy": true, "std.ptr.copy": true, "core.ptr.drop_in_place": true, "std.ptr.drop_in_place": true}
	for i, t := range tokens {
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(tokens) && tokens[end].text != ";" {
			end++
		}
		path := joinTokens(tokens[i+1 : end])
		for _, base := range []string{"core::ptr", "std::ptr"} {
			if path == base || strings.HasPrefix(path, base+"::{self,") {
				contracts["ptr.copy"] = true
				contracts["ptr.drop_in_place"] = true
			}
			if strings.HasPrefix(path, base+"as") {
				alias := strings.TrimPrefix(path, base+"as")
				contracts[alias+".copy"] = true
				contracts[alias+".drop_in_place"] = true
			}
		}
	}
	for i, t := range tokens {
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(tokens) && tokens[end].text != ";" {
			end++
		}
		path := joinTokens(tokens[i+1 : end])
		if strings.HasPrefix(path, "core::ptr") || strings.HasPrefix(path, "std::ptr") {
			continue
		}
		for key := range contracts {
			bound := strings.Split(key, ".")[0]
			if bound == "core" || bound == "std" {
				continue
			}
			for _, part := range tokens[i+1 : end] {
				if part.text == bound {
					delete(contracts, key)
				}
			}
		}
	}
	// A conflicting local module or function keeps the purported library binding
	// unresolved rather than awarding credit from its spelling.
	for i, t := range tokens {
		if (t.text == "mod" || t.text == "fn") && i+1 < len(tokens) {
			name := tokens[i+1].text
			for key := range contracts {
				if key == name || strings.HasPrefix(key, name+".") {
					delete(contracts, key)
				}
			}
		}
	}
	return contracts
}

func gradedRustPointerEffects(body, source []token, fields map[string]gradedRustFlow, prefix string) (destroyed, moved bool) {
	contracts := gradedRustPointerContracts(source)
	locals := map[string]gradedRustFlow{"self": {kind: "owner", identity: "self"}}
	body = gradedRustEagerResourceBody(body)
	for i, t := range body {
		if t.text == "return" && gradedUnconditional(body, i) {
			body = body[:statementEnd(body, i)]
			break
		}
	}
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	declarations := gradedOutputDeclarations(body, "rust")
	scopes := []map[string]gradedRustFlow{{}}
	for i, t := range body {
		if t.text == "{" {
			scopes = append(scopes, map[string]gradedRustFlow{})
		}
		if t.text == "}" && len(scopes) > 1 {
			for name, old := range scopes[len(scopes)-1] {
				if old.kind == "" {
					delete(locals, name)
				} else {
					locals[name] = old
				}
			}
			scopes = scopes[:len(scopes)-1]
		}
		if decl, ok := declarations[i]; ok {
			scope := scopes[len(scopes)-1]
			if _, seen := scope[t.text]; !seen {
				scope[t.text] = locals[t.text]
			}
			locals[t.text] = gradedRustExpressionFlow(decl.initializer, fields, locals)
			if gradedRustManuallyDropSelf(decl.initializer, source) {
				locals[t.text] = gradedRustFlow{kind: "owner", identity: "self"}
			}
		}
		if isIdentifier(t.text) && i+1 < len(body) && body[i+1].text == "=" && (i == 0 || body[i-1].text != ".") {
			if _, declared := declarations[i]; !declared {
				end := statementEnd(body, i+2)
				if gradedUnconditional(body, i) {
					locals[t.text] = gradedRustExpressionFlow(body[i+2:end], fields, locals)
				} else {
					delete(locals, t.text)
				}
			}
		}
		c, ok := calls[i]
		if !ok || !contracts[c.name] {
			continue
		}
		if strings.HasSuffix(c.name, ".drop_in_place") && len(c.actuals) == 1 && gradedUnconditional(body, c.position) {
			f := gradedRustExpressionFlow(c.actuals[0], fields, locals)
			if f.kind == "pointer" && len(f.fields) > 0 {
				destroyed = true
			}
		}
		if strings.HasSuffix(c.name, ".copy") && len(c.actuals) == 3 {
			a := gradedRustExpressionFlow(c.actuals[0], fields, locals)
			b := gradedRustExpressionFlow(c.actuals[1], fields, locals)
			if a.kind != "pointer" || b.kind != "pointer" || a.identity == b.identity || joinTokens(c.actuals[2]) == "0" {
				continue
			}
			for field := range a.fields {
				if b.fields[field] {
					moved = true
				}
			}
		}
	}
	return
}

// Unknown function/method calls are identity barriers. Only language raw-pointer
// operations and resolved standard pointer/container contracts preserve origin.
func gradedRustExpressionFlow(expression []token, fields map[string]gradedRustFlow, locals map[string]gradedRustFlow) gradedRustFlow {
	empty := gradedRustFlow{}
	for len(expression) > 1 && expression[0].text == "(" && matching(expression, 0, "(", ")") == len(expression)-1 {
		expression = expression[1 : len(expression)-1]
	}
	if len(expression) == 0 {
		return empty
	}
	flow, ok := locals[expression[0].text]
	if !ok || flow.kind == "" {
		return empty
	}
	i := 1
	for i < len(expression) {
		if expression[i].text != "." || i+1 >= len(expression) {
			return empty
		}
		name := expression[i+1].text
		i += 2
		if flow.kind == "owner" {
			field, ok := fields[name]
			if !ok {
				return empty
			}
			flow = field
			continue
		}
		if i+1 < len(expression) && expression[i].text == "::" && expression[i+1].text == "<" {
			end := rustGenericEnd(expression, i+1)
			if end < 0 {
				return empty
			}
			i = end + 1
		}
		if i >= len(expression) || expression[i].text != "(" {
			return empty
		}
		end := matching(expression, i, "(", ")")
		if end < 0 {
			return empty
		}
		args := expression[i+1 : end]
		switch flow.kind {
		case "pointer":
			if name != "add" && name != "sub" && name != "offset" && name != "wrapping_add" && name != "wrapping_sub" && name != "cast" && name != "cast_slice" {
				return empty
			}
		case "nonnull":
			if name == "as_ptr" {
				flow.kind = "pointer"
			} else if (name == "as_mut" || name == "as_ref") && flow.inner != "" {
				flow.kind = flow.inner
			} else {
				return empty
			}
		case "vec":
			if name == "as_ptr" || name == "as_mut_ptr" {
				flow.kind = "pointer"
			} else if name == "as_slice" {
				flow.kind = "slice"
			} else {
				return empty
			}
		case "slice":
			if name == "as_ptr" || name == "as_mut_ptr" {
				flow.kind = "pointer"
			} else {
				return empty
			}
		case "iter":
			if name == "as_slice" {
				flow.kind = "slice"
			} else {
				return empty
			}
		default:
			return empty
		}
		offset := name == "add" || name == "sub" || name == "offset" || name == "wrapping_add" || name == "wrapping_sub"
		if name != "cast" && name != "cast_slice" && !(offset && joinTokens(args) == "0") {
			flow.identity += "." + name + "(" + joinTokens(args) + ")"
		}
		if len(flow.identity) > 2048 {
			return empty
		}
		i = end + 1
	}
	return flow
}

func gradedRustPointerFields(u unit, owner string) map[string]gradedRustFlow {
	result := map[string]gradedRustFlow{}
	decl := gradedFindSurfaceOwner(u.tokens, "rust", owner)
	if decl == nil {
		return result
	}
	for i := decl.open + 1; i+2 < decl.close; i++ {
		if u.tokens[i+1].text != ":" {
			continue
		}
		name := u.tokens[i].text
		if _, ok := decl.fields[name]; !ok {
			continue
		}
		end := i + 2
		depth := 0
		for end < decl.close {
			switch u.tokens[end].text {
			case "<":
				depth++
			case ">":
				depth--
			case ">>":
				depth -= 2
			}
			if u.tokens[end].text == "," && depth == 0 {
				break
			}
			end++
		}
		typ := u.tokens[i+2 : end]
		shadowed := false
		for j := 0; j+2 < decl.open; j++ {
			if u.tokens[j].text == "struct" && u.tokens[j+1].text == owner && u.tokens[j+2].text == "<" {
				for name := range gradedRustGenericBindings(u.tokens[j+2 : decl.open]) {
					if len(typ) > 0 && typ[0].text == name {
						shadowed = true
					}
				}
			}
		}
		if shadowed {
			continue
		}
		kind, inner := gradedRustPointerType(typ, u.tokens)
		if kind != "" {
			result[name] = gradedRustFlow{kind: kind, inner: inner, identity: "self." + name, fields: map[string]bool{name: true}}
		}
	}
	return result
}
func gradedRustPointerType(typ, source []token) (kind, inner string) {
	if len(typ) > 0 && typ[0].text == "*" {
		return "pointer", ""
	}
	name := rustImplPathName(typ)
	pathEnd := len(typ)
	for i, t := range typ {
		if t.text == "<" {
			pathEnd = i
			break
		}
	}
	path := joinTokens(typ[:pathEnd])
	qualified := strings.Contains(path, "::")
	if qualified && path != "core::ptr::NonNull" && path != "std::ptr::NonNull" && path != "std::vec::Vec" && path != "alloc::vec::Vec" {
		return "", ""
	}
	if name == "NonNull" && (qualified || gradedRustImportedType(source, "NonNull", []string{"core::ptr", "std::ptr"})) {
		kind = "nonnull"
		for i, t := range typ {
			if t.text == "<" {
				end := rustGenericEnd(typ, i)
				if end > i {
					inner, _ = gradedRustPointerType(typ[i+1:end], source)
				}
				break
			}
		}
		return
	}
	if name == "Vec" && (qualified || gradedRustImportedType(source, "Vec", []string{"std::vec", "alloc::vec"})) {
		return "vec", ""
	}
	return "", ""
}
func gradedRustImportedType(source []token, name string, packages []string) bool {
	found := false
	for i, t := range source {
		if (t.text == "struct" || t.text == "enum" || t.text == "type") && i+1 < len(source) && source[i+1].text == name {
			return false
		}
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(source) && source[end].text != ";" {
			end++
		}
		contains := false
		for _, part := range source[i+1 : end] {
			contains = contains || part.text == name
		}
		if !contains {
			continue
		}
		path := joinTokens(source[i+1 : end])
		valid := false
		for _, pkg := range packages {
			valid = valid || strings.HasPrefix(path, pkg+"::")
		}
		if !valid {
			return false
		}
		found = true
	}
	return found
}

func gradedRustManuallyDropSelf(body, source []token) bool {
	if joinTokens(body) != "ManuallyDrop::new(self)" {
		return false
	}
	for i, t := range source {
		if (t.text == "struct" || t.text == "type" || t.text == "mod") && i+1 < len(source) && source[i+1].text == "ManuallyDrop" {
			return false
		}
		if t.text == "use" {
			end := i + 1
			for end < len(source) && source[end].text != ";" {
				end++
			}
			path := joinTokens(source[i+1 : end])
			if (strings.HasPrefix(path, "core::mem::") || strings.HasPrefix(path, "std::mem::")) && strings.Contains(path, "ManuallyDrop") {
				return true
			}
		}
	}
	return false
}

func gradedRustEagerResourceBody(body []token) []token {
	result := []token{}
	for i := 0; i < len(body); i++ {
		if body[i].text == "async" {
			start := i + 1
			if start < len(body) && body[start].text == "move" {
				start++
			}
			if start < len(body) && body[start].text == "{" {
				if end := matching(body, start, "{", "}"); end > start {
					i = end
					continue
				}
			}
		}
		if body[i].text == "||" && i > 0 && (body[i-1].text == "=" || body[i-1].text == "move" || body[i-1].text == "(") {
			start := i + 1
			end := statementEnd(body, start)
			if start < len(body) && body[start].text == "{" {
				end = matching(body, start, "{", "}") + 1
			}
			if end > start {
				i = end - 1
				continue
			}
		}
		if body[i].text == "struct" || body[i].text == "impl" || body[i].text == "fn" {
			end := i + 1
			for end < len(body) && body[end].text != "{" && body[end].text != ";" {
				end++
			}
			if end < len(body) && body[end].text == "{" {
				end = matching(body, end, "{", "}")
			}
			if end >= i && end < len(body) {
				i = end
				continue
			}
		}
		result = append(result, body[i])
	}
	return gradedEagerBody(pruneDeadFalseBranches(result), "rust")
}

func gradedRustGenericBindings(header []token) map[string]bool {
	result := map[string]bool{}
	depth := 0
	expect := false
	for _, t := range header {
		switch t.text {
		case "<":
			depth++
			if depth == 1 {
				expect = true
			}
		case ">":
			depth--
		case ">>":
			depth -= 2
		case ",":
			if depth == 1 {
				expect = true
			}
		default:
			if expect && depth == 1 {
				if isIdentifier(t.text) {
					result[t.text] = true
				}
				expect = false
			}
		}
		if depth <= 0 {
			break
		}
	}
	return result
}

// Only resolved, private, no-argument self helpers used as statements are
// expanded. Lexical blocks retain local binding scopes; returns remain barriers.
func gradedRustExpandHelpers(body []token, u unit, owner string, depth int, seen map[string]bool) []token {
	if depth >= maxCallDepth {
		return body
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	result := []token{}
	for i := 0; i < len(body); i++ {
		if i+4 < len(body) && body[i].text == "self" && body[i+1].text == "." && body[i+3].text == "(" && body[i+4].text == ")" && (i+5 == len(body) || body[i+5].text == ";" || body[i+5].text == "}") && (i == 0 || body[i-1].text == ";" || body[i-1].text == "{" || body[i-1].text == "}") {
			var helper *operation
			ambiguous := false
			for _, op := range rustOperations(u.file, u.index, u.tokens, u.pkg, rustFunctions(u.tokens)) {
				if op.owner == owner && op.name == body[i+2].text && !op.exposed && op.params == 0 && gradedRustSynchronousHelper(u.tokens, owner, op.name) {
					if helper != nil {
						ambiguous = true
					}
					helper = op
				}
			}
			if helper != nil && !ambiguous && !seen[helper.id] {
				safe := true
				for _, t := range helper.body {
					if t.text == "return" {
						safe = false
					}
				}
				if safe {
					seen[helper.id] = true
					expanded := gradedRustExpandHelpers(helper.body, u, owner, depth+1, seen)
					delete(seen, helper.id)
					result = append(result, token{text: "{"})
					result = append(result, expanded...)
					result = append(result, token{text: "}"})
					i += 4
					continue
				}
			}
		}
		result = append(result, body[i])
	}
	return result
}

// A discarded async invocation only creates a future. Restrict expansion to a
// unique inherent synchronous declaration with the actual self receiver.
func gradedRustSynchronousHelper(source []token, owner, name string) bool {
	matches := 0
	for _, fn := range rustFunctions(source) {
		if fn.owner != owner || fn.name != name {
			continue
		}
		matches++
		if fn.impl == nil || fn.impl.trait != "" {
			return false
		}
		receiver := false
		for _, t := range source[fn.paramOpen+1 : fn.paramClose] {
			if t.text == "self" {
				receiver = true
			}
		}
		if !receiver {
			return false
		}
		for i := fn.start - 1; i >= 0; i-- {
			if source[i].text == "async" {
				return false
			}
			if source[i].text == "{" || source[i].text == "}" || source[i].text == ";" {
				break
			}
		}
	}
	return matches == 1
}
