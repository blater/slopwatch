package sourceestimate

func gradedCachedFactories(op *operation, u unit, units []unit, byKey map[string][]*operation) int {
	body := trimSemicolonTokens(op.body)
	if len(body) < 8 || body[0].text != "return" {
		return 0
	}
	fields := callerDeclaredFields(u, op.owner)
	index := 1
	if index+2 < len(body) && body[index].text == "this" && body[index+1].text == "." {
		index += 2
	}
	field, ok := fields[body[index].text]
	if !ok || !gradeOwnedFieldReference(op, u, body, index) || field.public || field.packageVisible || !gradedMapType(u, field.typeName) {
		return 0
	}
	index++
	count := 0
	for index+3 < len(body) && body[index].text == "." && body[index+1].text == "computeIfAbsent" && body[index+2].text == "(" {
		close := matching(body, index+2, "(", ")")
		if close < 0 {
			return 0
		}
		args := body[index+3 : close]
		arrow := -1
		for i, t := range args {
			if t.text == "->" {
				arrow = i + 1
				break
			}
			if t.text == "-" && i+1 < len(args) && args[i+1].text == ">" {
				arrow = i + 2
				break
			}
		}
		if arrow < 0 {
			return 0
		}
		factory := args[arrow:]
		producedType, valid := gradedConnectedFactoryValue(op, factory, units, byKey, 0)
		if !valid {
			return 0
		}
		count++
		index = close + 1
		// A following lookup is a Map operation only if this factory constructs a
		// declared Map, proving the returned receiver rather than name chaining.
		if index < len(body) && !gradedMapType(u, producedType) {
			return 0
		}
	}
	if index != len(body) {
		return 0
	}
	return count
}
func gradedFactoryValue(body []token) (string, bool) {
	if len(body) > 2 && body[0].text == "new" && isIdentifier(body[1].text) {
		return body[1].text, true
	}
	if len(body) < 8 || body[0].text != "{" || matching(body, 0, "{", "}") != len(body)-1 {
		return "", false
	}
	inner := trimSemicolonTokens(body[1 : len(body)-1])
	name, typ := "", ""
	for i := 2; i+2 < len(inner); i++ {
		if inner[i].text == "=" && inner[i+1].text == "new" && isIdentifier(inner[i-1].text) {
			if name != "" {
				return "", false
			}
			name = inner[i-1].text
			typ = inner[i+2].text
		}
	}
	if name == "" || len(inner) < 2 || inner[len(inner)-2].text != "return" || inner[len(inner)-1].text != name {
		return "", false
	}
	assigned := 0
	for i, t := range inner {
		if t.text == name && i+1 < len(inner) && gradedStorageWriteAt(inner, i) {
			assigned++
		}
		if t.text == "if" || t.text == "for" || t.text == "while" || t.text == "try" || t.text == "throw" {
			return "", false
		}
	}
	return typ, assigned == 1
}
func gradedConnectedFactoryValue(op *operation, body []token, units []unit, byKey map[string][]*operation, depth int) (string, bool) {
	if typ, ok := gradedFactoryValue(body); ok {
		return typ, true
	}
	if depth >= maxCallDepth {
		return "", false
	}
	expression := trimSemicolonTokens(body)
	if len(expression) > 0 && expression[0].text == "return" {
		expression = expression[1:]
	}
	calls := callsIn(expression)
	if len(calls) != 1 {
		return "", false
	}
	c := calls[0]
	start := c.position
	for start >= 2 && expression[start-1].text == "." {
		start -= 2
	}
	if start != 0 || c.position+1 >= len(expression) || matching(expression, c.position+1, "(", ")") != len(expression)-1 {
		return "", false
	}
	matches := resolveCall(op, c, units, byKey)
	if len(matches) != 1 || matches[0].owner != op.owner || matches[0].exposed {
		return "", false
	}
	callee := matches[0]
	inner := trimSemicolonTokens(callee.body)
	if len(inner) > 0 && inner[0].text == "return" {
		return gradedConnectedFactoryValue(callee, inner[1:], units, byKey, depth+1)
	}
	wrapped := append([]token{{text: "{"}}, inner...)
	wrapped = append(wrapped, token{text: "}"})
	return gradedFactoryValue(wrapped)
}
