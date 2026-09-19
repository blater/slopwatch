package sourceestimate

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
