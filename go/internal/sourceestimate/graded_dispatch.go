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
		supported, returns := gradedDispatchArmBehavior(statements, alias, selector, op.paramNames)
		if !supported {
			return "", false
		}
		forwarded = forwarded || returns
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

func gradedDispatchArmBehavior(statements [][]token, alias, selector string, params []string) (supported, forwarded bool) {
	if len(statements) == 1 {
		s := statements[0]
		if len(s) > 1 && s[0].text == "return" && gradedDispatchCall(s[1:], alias, params) {
			return true, true
		}
		// A loop-carried receiver may unwrap one implementation layer. This
		// is local capability discovery, not a converted result.
		if len(s) > 2 && s[0].text == selector && s[1].text == "=" && gradedDispatchCall(s[2:], alias, nil) {
			return true, false
		}
	}
	if len(statements) == 2 && gradedDispatchCall(statements[0], alias, params) && joinTokens(statements[1]) == "returnnil" {
		return true, true
	}
	return false, false
}
