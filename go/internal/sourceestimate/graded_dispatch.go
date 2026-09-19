package sourceestimate

import (
	"sort"
	"strings"
)

// A capability dispatch chooses an implementation without converting its inputs
// or outputs. Its tested capabilities remain validation obligations; they are
// not representation transformations just because a type switch contains calls.
func gradedCapabilityDispatch(op *operation, body []token) (string, bool) {
	if op.language != "go" {
		return "", false
	}
	switchAt := -1
	for i, tok := range body {
		if tok.text == "switch" {
			if switchAt >= 0 {
				return "", false
			}
			switchAt = i
		}
	}
	if switchAt < 0 {
		return "", false
	}
	open := switchAt + 1
	for open < len(body) && body[open].text != "{" {
		open++
	}
	if open >= len(body) {
		return "", false
	}
	header := body[switchAt+1 : open]
	if len(header) < 7 || !isIdentifier(header[0].text) || header[1].text != ":=" || joinTokens(header[len(header)-4:]) != ".(type)" {
		return "", false
	}
	alias := header[0].text
	receiver := header[2 : len(header)-4]
	if !gradedDispatchPath(receiver) {
		return "", false
	}
	selector := joinTokens(receiver)
	prefix := body[:switchAt]
	if len(prefix) >= 2 && prefix[len(prefix)-2].text == "for" && prefix[len(prefix)-1].text == "{" {
		prefix = prefix[:len(prefix)-2]
	}
	prefix = trimSemicolonTokens(prefix)
	if len(prefix) > 0 {
		if len(receiver) != 1 || len(prefix) < 3 || prefix[0].text != selector || prefix[1].text != ":=" || !gradedDispatchPath(prefix[2:]) {
			return "", false
		}
		receiver = prefix[2:]
	}
	close := matching(body, open, "{", "}")
	if close < 0 {
		return "", false
	}
	for _, tok := range body[close+1:] {
		if tok.text != "}" && tok.text != ";" {
			return "", false
		}
	}
	arms, ok := gradedDispatchArms(body[open+1 : close])
	if !ok {
		return "", false
	}
	capabilities := []string{}
	forwarded := false
	for _, arm := range arms {
		statements := gradedResultStatements(arm.body)
		if arm.kind == "default" {
			if len(statements) != 1 || len(statements[0]) < 2 || statements[0][0].text != "return" || !gradedDispatchFailure(statements[0][1:]) {
				return "", false
			}
			continue
		}
		capabilities = append(capabilities, joinTokens(arm.typ))
		if len(statements) == 1 {
			s := statements[0]
			if len(s) > 1 && s[0].text == "return" && gradedDispatchCall(s[1:], alias, op.paramNames) {
				forwarded = true
				continue
			}
			// A loop-carried receiver may unwrap one implementation layer. This
			// is local capability discovery, not a converted result.
			if len(s) > 2 && s[0].text == selector && s[1].text == "=" && gradedDispatchCall(s[2:], alias, nil) {
				continue
			}
		}
		if len(statements) == 2 && gradedDispatchCall(statements[0], alias, op.paramNames) && joinTokens(statements[1]) == "returnnil" {
			forwarded = true
			continue
		}
		return "", false
	}
	if !forwarded {
		return "", false
	}
	sort.Strings(capabilities)
	path := joinTokens(receiver)
	if op.receiverName != "" && strings.HasPrefix(path, op.receiverName+".") {
		path = "self." + strings.TrimPrefix(path, op.receiverName+".")
	} else {
		// Parameter/local identities do not persist across operations.
		path = op.id + ":" + path
	}
	return itoa(op.file) + ":" + op.owner + ":" + path + ":" + strings.Join(capabilities, "|"), true
}

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
