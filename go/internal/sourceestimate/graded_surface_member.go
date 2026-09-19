package sourceestimate

func gradedMemberMethodEnd(tokens []token, start, close int) int {
	paren := -1
	for i := start; i < close; i++ {
		switch tokens[i].text {
		case "=", ";":
			return 0
		case "(":
			paren = i
			break
		case "{":
			if paren < 0 {
				return 0
			}
		}
		if paren >= 0 {
			break
		}
	}
	if paren < 0 {
		return 0
	}
	paramsEnd := matching(tokens, paren, "(", ")")
	if paramsEnd < 0 {
		return 0
	}
	bodyStart := paramsEnd + 1
	for bodyStart < close && tokens[bodyStart].text != "{" {
		if tokens[bodyStart].text == ";" {
			return 0
		}
		bodyStart++
	}
	if bodyStart >= close {
		return 0
	}
	bodyEnd := matching(tokens, bodyStart, "{", "}")
	if bodyEnd < 0 {
		return 0
	}
	return bodyEnd + 1
}
