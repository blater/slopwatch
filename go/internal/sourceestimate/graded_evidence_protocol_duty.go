package sourceestimate

func protocolCallerDuty(item evidenceItem, u unit, prerequisites map[string]bool) bool {
	switch item.category {
	case "resource", "coordination", "state":
		return true
	case "validation":
		body := item.origin.body
		guards := 0
		for i, tok := range body {
			if tok.text != "if" && tok.text != "assert" && tok.text != "assert_eq" {
				continue
			}
			start := i + 1
			if start < len(body) && body[start].text == "!" {
				start++
			}
			end := start
			if start < len(body) && body[start].text == "(" {
				end = matching(body, start, "(", ")")
			} else {
				for end < len(body) && body[end].text != "{" {
					end++
				}
			}
			if end < start {
				continue
			}
			governed := false
			for j := start; j < end; j++ {
				for _, parameter := range item.origin.paramNames {
					if body[j].text == parameter && (j == 0 || body[j-1].text != ".") {
						return false
					}
				}
				if prerequisites[body[j].text] && gradeOwnedFieldReference(item.origin, u, body, j) {
					governed = true
				}
			}
			if !governed {
				return false
			}
			guards++
		}
		return guards > 0
	}
	return false
}
