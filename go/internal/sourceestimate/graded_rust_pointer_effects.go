package sourceestimate

import "strings"

func gradedRustPointerEffects(body, source []token, fields map[string]gradedRustFlow, prefix string) (destroyed, moved bool) {
	contracts := gradedRustPointerContracts(source)
	locals := map[string]gradedRustFlow{"self": {kind: "owner", identity: "self"}}
	body = gradedRustEagerResourceBody(body)
	for i, t := range body {
		if t.text == "return" && gradedUnconditional(body, i) {
			body = body[:statementEnd(body, i)]
			break
		}
	}
	calls := map[int]call{}
	for _, c := range callsIn(body) {
		calls[c.position] = c
	}
	declarations := gradedOutputDeclarations(body, "rust")
	scopes := []map[string]gradedRustFlow{{}}
	for i, t := range body {
		destroyedNow, movedNow := gradedRustPointerToken(body, source, fields, contracts, declarations, calls, locals, &scopes, i, t)
		destroyed, moved = destroyed || destroyedNow, moved || movedNow
	}
	return
}

func gradedRustPointerToken(body, source []token, fields map[string]gradedRustFlow, contracts map[string]bool, declarations map[int]gradedOutputDeclaration, calls map[int]call, locals map[string]gradedRustFlow, scopes *[]map[string]gradedRustFlow, i int, t token) (destroyed, moved bool) {
	if t.text == "{" {
		*scopes = append(*scopes, map[string]gradedRustFlow{})
	}
	if t.text == "}" && len(*scopes) > 1 {
		for name, old := range (*scopes)[len(*scopes)-1] {
			if old.kind == "" {
				delete(locals, name)
			} else {
				locals[name] = old
			}
		}
		*scopes = (*scopes)[:len(*scopes)-1]
	}
	if decl, ok := declarations[i]; ok {
		scope := (*scopes)[len(*scopes)-1]
		if _, seen := scope[t.text]; !seen {
			scope[t.text] = locals[t.text]
		}
		locals[t.text] = gradedRustExpressionFlow(decl.initializer, fields, locals)
		if gradedRustManuallyDropSelf(decl.initializer, source) {
			locals[t.text] = gradedRustFlow{kind: "owner", identity: "self"}
		}
	}
	if isIdentifier(t.text) && i+1 < len(body) && body[i+1].text == "=" && (i == 0 || body[i-1].text != ".") {
		if _, declared := declarations[i]; !declared {
			end := statementEnd(body, i+2)
			if gradedUnconditional(body, i) {
				locals[t.text] = gradedRustExpressionFlow(body[i+2:end], fields, locals)
			} else {
				delete(locals, t.text)
			}
		}
	}
	c, ok := calls[i]
	if !ok || !contracts[c.name] {
		return false, false
	}
	return gradedRustPointerCall(body, fields, locals, c)
}

func gradedRustPointerCall(body []token, fields map[string]gradedRustFlow, locals map[string]gradedRustFlow, c call) (destroyed, moved bool) {
	if strings.HasSuffix(c.name, ".drop_in_place") && len(c.actuals) == 1 && gradedUnconditional(body, c.position) {
		f := gradedRustExpressionFlow(c.actuals[0], fields, locals)
		if f.kind == "pointer" && len(f.fields) > 0 {
			destroyed = true
		}
	}
	if !strings.HasSuffix(c.name, ".copy") || len(c.actuals) != 3 {
		return destroyed, false
	}
	a := gradedRustExpressionFlow(c.actuals[0], fields, locals)
	b := gradedRustExpressionFlow(c.actuals[1], fields, locals)
	if a.kind != "pointer" || b.kind != "pointer" || a.identity == b.identity || joinTokens(c.actuals[2]) == "0" {
		return destroyed, false
	}
	for field := range a.fields {
		if b.fields[field] {
			moved = true
		}
	}
	return destroyed, moved
}
