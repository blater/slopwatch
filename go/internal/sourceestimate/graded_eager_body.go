package sourceestimate

// Callable definitions are not eager effects. Remove only their bounded body,
// preserving subsequent statements and mutations before/after unrelated lambdas.
// The result is read-only; unchanged input is returned as a capped shared view.
func gradedEagerBody(body []token, language string) []token {
	var result []token
	copyPrefix := func(end int) {
		if result == nil {
			result = make([]token, end, len(body))
			copy(result, body[:end])
		}
	}
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if end, ok := gradedEagerCallableEnd(body, i, t); ok {
			copyPrefix(i)
			i = end
			continue
		}
		if language == "rust" {
			if end, ok := gradedEagerRustClosureEnd(body, i, t); ok {
				copyPrefix(i)
				result = append(result, token{text: "null", offset: body[i].offset, line: body[i].line})
				i = end - 1
				continue
			}
		}
		if end, ok := gradedEagerPipeClosureEnd(body, i, language, t); ok {
			copyPrefix(i)
			result = append(result, token{text: "null"})
			i = end - 1
			continue
		}
		if language != "rust" {
			if end, ok := gradedEagerArrowEnd(body, i, t); ok {
				copyPrefix(i)
				result = append(result, token{text: "null"})
				i = end - 1
				continue
			}
		}
		if result != nil {
			result = append(result, body[i])
		}
	}
	if result == nil {
		if body == nil {
			return []token{}
		}
		return body[:len(body):len(body)]
	}
	return result[:len(result):len(result)]
}

func gradedEagerCallableEnd(body []token, i int, text string) (int, bool) {
	if text != "function" && text != "func" {
		return 0, false
	}
	open := i + 1
	for open < len(body) && body[open].text != "{" {
		open++
	}
	if open >= len(body) {
		return 0, false
	}
	end := matching(body, open, "{", "}")
	return end, end >= open
}

func gradedEagerRustClosureEnd(body []token, i int, text string) (int, bool) {
	if text != "||" || i == 0 || body[i-1].text != "=" && body[i-1].text != "(" && body[i-1].text != "," && body[i-1].text != "move" {
		return 0, false
	}
	return gradedEagerClosureEnd(body, i+1)
}

func gradedEagerPipeClosureEnd(body []token, i int, language, text string) (int, bool) {
	if text != "|" || i == 0 || body[i-1].text != "=" && body[i-1].text != "(" && body[i-1].text != "," && (language != "rust" || body[i-1].text != "move") {
		return 0, false
	}
	params := i + 1
	for params < len(body) && body[params].text != "|" {
		params++
	}
	if params >= len(body) {
		return 0, false
	}
	return gradedEagerClosureEnd(body, params+1)
}

func gradedEagerArrowEnd(body []token, i int, text string) (int, bool) {
	if text != "=>" && text != "->" && (text != "-" || i+1 >= len(body) || body[i+1].text != ">") {
		return 0, false
	}
	start := i + 1
	if text == "-" {
		start++
	}
	return gradedEagerClosureEnd(body, start)
}

func gradedEagerClosureEnd(body []token, start int) (int, bool) {
	end := statementEnd(body, start)
	if start < len(body) && body[start].text == "{" {
		end = matching(body, start, "{", "}") + 1
	}
	return end, end > start
}
