package sourceestimate

func rustSupportingOnly(tokens []token) bool {
	if len(tokens) == 0 || hasRustUncertainSyntax(tokens) {
		return false
	}
	return len(rustNamedRanges(tokens, "struct")) > 0 || len(rustNamedRanges(tokens, "enum")) > 0 || len(rustNamedRanges(tokens, "trait")) > 0
}

// rustSupportingTypeProof is the small structural proof shared by callers
// that need to keep value/trait files in inventory without treating them as
// behavioral roots.
func rustSupportingTypeProof(tokens []token) (string, bool) {
	if !rustSupportingOnly(tokens) {
		return "", false
	}
	if rustTraitOnly(tokens) {
		return roleSupportingRustTrait, true
	}
	return roleSupportingRustType, true
}

// supportingRustOwners proves only the affected receiver/type owner. A
// passive representation beside a service therefore cannot erase or inflate
// the service root. Trait implementations are intentionally excluded because
// their methods are behavior surfaces even when their bodies look like simple
// getters.
func supportingRustOwners(units []unit) map[attributionOwnerKey]string {
	result := map[attributionOwnerKey]string{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" || u.limited || !u.lexicallyValid {
			continue
		}
		shapes := rustNamedRanges(u.tokens, "struct")
		for _, shape := range shapes {
			if !rustStructHasFields(u.tokens, shape) {
				continue
			}
			var ownerOps []*operation
			for _, op := range u.ops {
				if op.owner != shape.name {
					continue
				}
				ownerOps = append(ownerOps, op)
			}
			if len(ownerOps) == 0 {
				continue
			}
			passive := true
			for _, op := range ownerOps {
				if !rustPassiveOperation(op.body) {
					passive = false
					break
				}
			}
			if passive {
				result[attributionOwnerKey{file: u.index, owner: shape.name}] = roleSupportingRustType
			}
		}
	}
	return result
}

func rustStructHasFields(tokens []token, shape rustRange) bool {
	if shape.end <= shape.start || shape.end >= len(tokens) {
		return false
	}
	open := shape.start + 2
	for open < shape.end && tokens[open].text != "{" {
		open++
	}
	if open >= shape.end {
		return false
	}
	for i := open + 1; i+1 < shape.end; i++ {
		if isIdentifier(tokens[i].text) && (tokens[i+1].text == ":" || tokens[i+1].text == ",") {
			return true
		}
	}
	return false
}

func rustPassiveOperation(body []token) bool {
	body = trimSemicolonTokens(body)
	if len(body) == 0 || gradeAssertion(body) || hasValidation(body) || hasTransform(body) || hasState(body) || len(callsIn(body)) != 0 {
		return false
	}
	// A constructor that only materializes the receiver's fields is part of a
	// passive value carrier. Keep the check deliberately narrow: computed
	// expressions and control flow belong to behavioral abstractions, while a
	// plain `Self { field: input }` literal is just representation transport.
	if len(body) >= 3 && body[0].text == "Self" && body[1].text == "{" {
		for _, item := range body[2:] {
			switch item.text {
			case "+", "-", "*", "/", "%", "&&", "||", "if", "match", "for", "while", "loop":
				return false
			}
		}
		return true
	}
	for i := 0; i+2 < len(body); i++ {
		if body[i].text == "self" && body[i+1].text == "." && isIdentifier(body[i+2].text) {
			return (i >= 1 && body[i-1].text == "return") || i == 0
		}
	}
	return false
}

func rustTraitOnly(tokens []token) bool {
	return len(rustNamedRanges(tokens, "trait")) > 0 && len(rustNamedRanges(tokens, "struct")) == 0 && len(rustNamedRanges(tokens, "enum")) == 0
}

func hasRustUncertainSyntax(tokens []token) bool {
	for _, item := range tokens {
		if item.text == "#" || item.text == "macro_rules" || item.text == "cfg" {
			return true
		}
	}
	return false
}

func rustRestrictedBefore(tokens []token, index int) bool {
	for i := index - 1; i >= 0 && tokens[i].text != "{" && tokens[i].text != "}" && tokens[i].text != ";"; i-- {
		if tokens[i].text == "pub" && i+1 < index && tokens[i+1].text == "(" {
			return true
		}
	}
	return false
}

// Only explicit test attributes exclude code; arbitrary cfg conditions retain
// their production uncertainty. Attribute scope includes an enclosing test mod.
func rustTestOnly(tokens []token, index int) bool {
	for i := 0; i+2 < index; i++ {
		if tokens[i].text != "#" || tokens[i+1].text != "[" {
			continue
		}
		end := matching(tokens, i+1, "[", "]")
		if end < 0 || end >= index {
			continue
		}
		attr := joinTokens(tokens[i+2 : end])
		if attr != "test" && attr != "cfg(test)" {
			continue
		}
		start := end + 1
		for start < len(tokens) && tokens[start].text != "{" && tokens[start].text != ";" {
			start++
		}
		if start < len(tokens) && tokens[start].text == "{" {
			close := matching(tokens, start, "{", "}")
			if index < close {
				return true
			}
		}
	}
	return false
}
