package sourceestimate

// Frozen pre-optimization normalization loops (49e4c52630b9). Recognizers are unchanged.
func referenceEagerBody(body []token, language string) []token {
	result := []token{}
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if end, ok := gradedEagerCallableEnd(body, i, t); ok {
			i = end
			continue
		}
		if language == "rust" {
			if end, ok := gradedEagerRustClosureEnd(body, i, t); ok {
				result = append(result, token{text: "null", offset: body[i].offset, line: body[i].line})
				i = end - 1
				continue
			}
		}
		if end, ok := gradedEagerPipeClosureEnd(body, i, language, t); ok {
			result = append(result, token{text: "null"})
			i = end - 1
			continue
		}
		if language != "rust" {
			if end, ok := gradedEagerArrowEnd(body, i, t); ok {
				result = append(result, token{text: "null"})
				i = end - 1
				continue
			}
		}
		result = append(result, body[i])
	}
	return result
}

func referencePruneDeadFalseBranches(body []token) []token {
	if len(body) == 0 {
		return body
	}
	result := make([]token, 0, len(body))
	for index := 0; index < len(body); {
		if body[index].text != "if" {
			result = append(result, body[index])
			index++
			continue
		}
		end, ok := deadFalseBranchEnd(body, index)
		if !ok {
			result = append(result, body[index])
			index++
			continue
		}
		index = end
	}
	return result
}
