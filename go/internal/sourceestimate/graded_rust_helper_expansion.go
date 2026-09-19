package sourceestimate

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
			for _, op := range rustUnitRawMembers(u, owner, body[i+2].text) {
				if op.owner == owner && op.name == body[i+2].text && !op.exposed && op.params == 0 && gradedRustUnitSynchronousHelper(u, owner, op.name) {
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
