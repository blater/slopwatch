package sourceestimate

func annotateOutputObligations(units []unit, index *operationLookup) {
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
