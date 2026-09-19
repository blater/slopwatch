package sourceestimate

import (
	"sort"
	"strings"
)

// Observable mutation through a caller-supplied buffer leaves allocation,
// retention and reset/combination with the caller. This is alias burden, not
// hidden responsibility or coupling inferred from parameter count.
func gradedCallerOutputBuffers(op *operation, units []unit, index map[string][]*operation, depth int) []string {
	if depth >= maxCallDepth || op.file < 0 || op.file >= len(units) {
		return nil
	}
	if index == nil {
		index = operationIndex(units)
	}
	if op.outputAssessed {
		return op.outputBuffers
	}
	op.outputAssessed = true
	u := units[op.file]
	body := gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)
	for i, t := range body {
		if t.text == "return" && gradedUnconditional(body, i) {
			body = body[:statementEnd(body, i)]
			break
		}
	}
	kinds := map[string]string{}
	aliases := map[string]string{}
	for name, typ := range op.parameterTypes {
		vector := op.language == "rust" && strings.HasPrefix(typ, "&mutVec<")
		array := op.language == "typescript" && (strings.HasSuffix(typ, "[]") || strings.HasPrefix(typ, "Array<"))
		slice := op.language == "go" && strings.HasPrefix(typ, "[]")
		if vector {
			for i, t := range u.tokens {
				if (t.text == "struct" || t.text == "type" || t.text == "enum") && i+1 < len(u.tokens) && u.tokens[i+1].text == "Vec" {
					vector = false
				}
				if t.text == "Vec" && i > 0 && i+1 < len(u.tokens) && u.tokens[i-1].text == "<" && (u.tokens[i+1].text == ">" || u.tokens[i+1].text == ":" || u.tokens[i+1].text == ",") {
					vector = false
				}
				if t.text == "use" {
					end := i + 1
					for end < len(u.tokens) && u.tokens[end].text != ";" {
						end++
					}
					path := joinTokens(u.tokens[i+1 : end])
					if strings.Contains(path, "Vec") && path != "std::vec::Vec" && path != "alloc::vec::Vec" {
						vector = false
					}
				}
			}
		}
		if vector {
			kinds[name] = "vector"
		}
		if array {
			kinds[name] = "array"
		}
		if slice {
			kinds[name] = "slice"
		}
		if kinds[name] != "" {
			aliases[name] = name
		}
	}
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	outputs := map[string]bool{}
	declarations := gradedOutputDeclarations(body, op.language)
	scopes := []map[string]string{{}}
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
			if declaration.block && len(scopes) > 1 {
				scope := scopes[len(scopes)-1]
				if _, seen := scope[t.text]; !seen {
					scope[t.text] = aliases[t.text]
				}
			}
			root := ""
			rhs := declaration.initializer
			if len(rhs) == 1 {
				root = aliases[rhs[0].text]
			}
			if root == "" {
				delete(aliases, t.text)
			} else {
				aliases[t.text] = root
			}
		} else if isIdentifier(t.text) && (i == 0 || body[i-1].text != ".") && i+1 < len(body) && (body[i+1].text == "=" || body[i+1].text == ":=") {
			declaration := i > 0 && (body[i-1].text == "const" || body[i-1].text == "let" || body[i-1].text == "mut") || body[i+1].text == ":="
			if declaration && len(scopes) > 1 {
				scope := scopes[len(scopes)-1]
				if _, seen := scope[t.text]; !seen {
					scope[t.text] = aliases[t.text]
				}
			}
			root := ""
			if i+2 < len(body) && statementEnd(body, i+2) == i+3 && gradedUnconditional(body, i) {
				root = aliases[body[i+2].text]
			}
			if root == "" {
				delete(aliases, t.text)
			} else {
				aliases[t.text] = root
			}
		}
		root := aliases[t.text]
		if root != "" && (i == 0 || body[i-1].text != ".") {
			if i+1 < len(body) && body[i+1].text == "[" {
				end := matching(body, i+1, "[", "]")
				if end > i && end+1 < len(body) && (body[end+1].text == "=" || body[end+1].text == "+=") {
					outputs[root] = true
				}
			}
			if i+3 < len(body) && body[i+1].text == "." && body[i+3].text == "(" {
				method := body[i+2].text
				kind := kinds[root]
				if kind == "vector" && (method == "push" || method == "extend" || method == "insert" || method == "clear") || kind == "array" && (method == "push" || method == "splice") {
					outputs[root] = true
				}
			}
		}
		c, ok := calls[i]
		if !ok {
			continue
		}
		matches := resolveCall(op, c, units, index)
		if len(matches) != 1 || matches[0].exposed || matches[0].owner != op.owner {
			continue
		}
		callee := matches[0]
		mutated := gradedCallerOutputBuffers(callee, units, index, depth+1)
		for j, param := range callee.paramNames {
			if j >= len(c.actuals) {
				continue
			}
			found := false
			for _, name := range mutated {
				found = found || name == param
			}
			if !found {
				continue
			}
			actual := strings.TrimPrefix(strings.TrimPrefix(joinTokens(c.actuals[j]), "&mut"), "&")
			if root := aliases[actual]; root != "" {
				outputs[root] = true
			}
		}
	}
	result := []string{}
	for name := range outputs {
		result = append(result, name)
	}
	sort.Strings(result)
	op.outputBuffers = result
	return result
}

// Callable definitions are not eager effects. Remove only their bounded body,
// preserving subsequent statements and mutations before/after unrelated lambdas.
func gradedEagerBody(body []token, language string) []token {
	result := []token{}
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if t == "function" || t == "func" {
			open := i + 1
			for open < len(body) && body[open].text != "{" {
				open++
			}
			if open < len(body) {
				end := matching(body, open, "{", "}")
				if end >= open {
					i = end
					continue
				}
			}
		}
		if language == "rust" && t == "||" && i > 0 && (body[i-1].text == "=" || body[i-1].text == "(" || body[i-1].text == "," || body[i-1].text == "move") {
			start := i + 1
			end := statementEnd(body, start)
			if start < len(body) && body[start].text == "{" {
				end = matching(body, start, "{", "}") + 1
			}
			if end > start {
				result = append(result, token{text: "null", offset: body[i].offset, line: body[i].line})
				i = end - 1
				continue
			}
		}
		if t == "|" && i > 0 && (body[i-1].text == "=" || body[i-1].text == "(" || body[i-1].text == "," || language == "rust" && body[i-1].text == "move") {
			params := i + 1
			for params < len(body) && body[params].text != "|" {
				params++
			}
			if params < len(body) {
				start := params + 1
				end := statementEnd(body, start)
				if start < len(body) && body[start].text == "{" {
					end = matching(body, start, "{", "}") + 1
				}
				if end > start {
					result = append(result, token{text: "null"})
					i = end - 1
					continue
				}
			}
		}
		if language != "rust" && (t == "=>" || t == "->" || t == "-" && i+1 < len(body) && body[i+1].text == ">") {
			start := i + 1
			if t == "-" {
				start++
			}
			end := statementEnd(body, start)
			if start < len(body) && body[start].text == "{" {
				end = matching(body, start, "{", "}") + 1
			}
			if end > start {
				result = append(result, token{text: "null"})
				i = end - 1
				continue
			}
		}
		result = append(result, body[i])
	}
	return result
}

func annotateOutputObligations(units []unit, index map[string][]*operation) {
	for _, u := range units {
		for _, op := range u.ops {
			gradedCallerOutputBuffers(op, units, index, 0)
		}
	}
}

type gradedOutputDeclaration struct {
	initializer []token
	block       bool
}

// Declaration identity is independent of the intervening type syntax. A
// declaration without initialization shadows an outer binding just as one
// with an initializer does; simple typed aliases preserve the existing buffer.
func gradedOutputDeclarations(body []token, language string) map[int]gradedOutputDeclaration {
	result := map[int]gradedOutputDeclaration{}
	for i, t := range body {
		if t.text != "let" && t.text != "const" && t.text != "var" {
			continue
		}
		name := i + 1
		if name < len(body) && body[name].text == "mut" {
			name++
		}
		if name >= len(body) || !isIdentifier(body[name].text) {
			continue
		}
		end := statementEnd(body, name)
		assign := -1
		for j := name + 1; j < end; j++ {
			if body[j].text == "=" {
				assign = j
				break
			}
		}
		entry := gradedOutputDeclaration{block: language != "typescript" || t.text != "var"}
		if assign >= 0 {
			entry.initializer = body[assign+1 : end]
		}
		result[name] = entry
		// Go's grouped identifiers share a type declaration; without independently
		// resolved initializer positions, shadow each conservatively.
		if language == "go" {
			for j := name + 1; j+1 < end && body[j].text == ","; j += 2 {
				result[j+1] = gradedOutputDeclaration{block: true}
			}
		}
	}
	return result
}
