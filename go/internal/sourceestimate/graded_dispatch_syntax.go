package sourceestimate

type gradedDispatchArm struct {
	kind      string
	typ, body []token
}

func gradedDispatchArms(body []token) ([]gradedDispatchArm, bool) {
	arms := []gradedDispatchArm{}
	for start := 0; start < len(body); {
		if body[start].text == ";" {
			start++
			continue
		}
		kind := body[start].text
		if kind != "case" && kind != "default" {
			return nil, false
		}
		colon, depth := start+1, 0
		for ; colon < len(body); colon++ {
			t := body[colon].text
			if t == ":" && depth == 0 {
				break
			}
			if t == "(" || t == "{" || t == "[" {
				depth++
			}
			if t == ")" || t == "}" || t == "]" {
				depth--
			}
		}
		if colon == len(body) {
			return nil, false
		}
		end := colon + 1
		for ; end < len(body); end++ {
			t := body[end].text
			if depth == 0 && (t == "case" || t == "default") {
				break
			}
			if t == "(" || t == "{" || t == "[" {
				depth++
			}
			if t == ")" || t == "}" || t == "]" {
				depth--
			}
		}
		arms = append(arms, gradedDispatchArm{kind, body[start+1 : colon], trimSemicolonTokens(body[colon+1 : end])})
		start = end
	}
	return arms, len(arms) > 0
}

func gradedDispatchPath(body []token) bool {
	if len(body) == 0 || len(body)%2 == 0 {
		return false
	}
	for i, tok := range body {
		if i%2 == 0 && !isIdentifier(tok.text) || i%2 == 1 && tok.text != "." {
			return false
		}
	}
	return true
}

func gradedDispatchCall(body []token, receiver string, params []string) bool {
	if len(body) < 5 || body[0].text != receiver || body[1].text != "." || !isIdentifier(body[2].text) || body[3].text != "(" || matching(body, 3, "(", ")") != len(body)-1 {
		return false
	}
	for _, arg := range splitArguments(body[4 : len(body)-1]) {
		if len(arg) == 0 {
			continue
		}
		if len(arg) != 1 {
			return false
		}
		found := false
		for _, param := range params {
			found = found || param == arg[0].text
		}
		if !found {
			return false
		}
	}
	return true
}

func gradedDispatchFailure(body []token) bool {
	// A default may report unavailable capability, including tuple nils. No
	// argument-dependent computation or receiver calls are accepted here.
	for _, part := range splitArguments(body) {
		if len(part) == 1 && (part[0].text == "nil" || isIdentifier(part[0].text)) {
			continue
		}
		if len(part) == 3 && isIdentifier(part[0].text) && part[1].text == "(" && part[2].text == ")" {
			continue
		}
		return false
	}
	return len(body) > 0
}
