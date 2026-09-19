package sourceestimate

// Frozen pre-index control-flow implementation, intentionally independent of the
// production index. Keep recursive behavior here only for bounded comparisons.
func frozenGradedUnconditional(body []token, position int) bool {
	return frozenGradedUnconditionalExits(body, position) && frozenGradedUnconditionalGuards(body, position)
}

func frozenGradedUnconditionalExits(body []token, position int) bool {
	depth := 0
	for i := 0; i < position; i++ {
		if body[i].text == "{" {
			depth++
		}
		if body[i].text == "}" {
			depth--
		}
		if frozenGradedUnconditionalExit(body, position, i, depth) {
			return false
		}
	}
	return true
}

func frozenGradedUnconditionalExit(body []token, position, i, depth int) bool {
	isExit := body[i].text == "return" || body[i].text == "throw" || body[i].text == "panic" && i+1 < len(body) && body[i+1].text == "("
	return depth == 0 && isExit && statementEnd(body, i) < position && frozenGradedUnconditional(body, i)
}

func frozenGradedUnconditionalGuards(body []token, position int) bool {
	for i := 0; i < position; i++ {
		if !frozenGradedGuardKeyword(body[i].text) {
			continue
		}
		start, ok := frozenGradedGuardStart(body, i)
		if !ok {
			return false
		}
		if start >= len(body) {
			continue
		}
		end := statementEnd(body, start)
		if body[start].text == "{" {
			end = matching(body, start, "{", "}")
		}
		if position >= start && position <= end {
			return false
		}
	}
	return true
}

func frozenGradedGuardKeyword(text string) bool {
	switch text {
	case "if", "else", "for", "while", "loop", "switch", "match":
		return true
	default:
		return false
	}
}

func frozenGradedGuardStart(body []token, keyword int) (int, bool) {
	start := keyword + 1
	if start < len(body) && body[start].text == "(" {
		end := matching(body, start, "(", ")")
		if end < 0 {
			return 0, false
		}
		return end + 1, true
	}
	if body[keyword].text != "else" {
		for start < len(body) && body[start].text != "{" {
			start++
		}
	}
	return start, true
}
