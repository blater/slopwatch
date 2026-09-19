package sourceestimate

import "strings"

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
func gradedRustSynchronousHelper(source []token, owner, name string) bool {
	return gradedRustSynchronousFunctions(source, owner, name, rustFunctions(source))
}
func gradedRustUnitSynchronousHelper(u unit, owner, name string) bool {
	return gradedRustSynchronousFunctions(u.tokens, owner, name, rustUnitMembers(u, owner, name))
}
func gradedRustSynchronousFunctions(source []token, owner, name string, functions []rustFunction) bool {
	matches := 0
	for _, fn := range functions {
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
