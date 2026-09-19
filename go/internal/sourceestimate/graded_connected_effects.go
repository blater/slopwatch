package sourceestimate

import "strings"

// Recognize only declared Java library contracts. A user type named Map or a
// method merely spelled computeIfAbsent/close never supplies this contract.
func gradedJavaLibraryType(u unit, typ, pkg string) bool {
	if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
		return false
	}
	for i, t := range u.tokens {
		if (t.text == "class" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(u.tokens) && u.tokens[i+1].text == typ {
			return false
		}
		if t.text == typ && i > 0 && i+1 < len(u.tokens) && (u.tokens[i-1].text == "<" || u.tokens[i-1].text == ",") && (u.tokens[i+1].text == "extends" || u.tokens[i+1].text == ">") {
			return false
		}
	}
	wildcard, explicit := false, false
	for i, t := range u.tokens {
		if t.text != "import" {
			continue
		}
		end := i + 1
		for end < len(u.tokens) && u.tokens[end].text != ";" {
			end++
		}
		name := joinTokens(u.tokens[i+1 : end])
		if strings.HasSuffix(name, "."+typ) {
			if name != pkg+"."+typ {
				return false
			}
			explicit = true
		}
		wildcard = wildcard || name == pkg+".*"
	}
	if !explicit && u.packageTypes[typ] {
		return false
	}
	return explicit || wildcard || pkg == "java.lang"
}

func gradedMapType(u unit, typ string) bool {
	return (typ == "Map" || typ == "HashMap" || typ == "IdentityHashMap" || typ == "LinkedHashMap") && gradedJavaLibraryType(u, typ, "java.util")
}

// The Map contract invokes a factory only on a missing key and retains the
// returned value. Count connected insertion layers, not callback body size.
// A disconnected lambda is never traversed or treated as an executed duty.
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

// JDBC close is a library ownership contract. Separate catch scopes establish
// that failure of one resource does not suppress the other cleanup attempt.
func gradedIndependentJDBCCleanup(op *operation, u unit, roots []*operation) bool {
	if op.language != "java" {
		return false
	}
	fields := callerDeclaredFields(u, op.owner)
	owned := map[string]bool{}
	for name, f := range fields {
		if f.typeName != "Statement" && f.typeName != "ResultSet" {
			continue
		}
		if !gradedJavaLibraryType(u, f.typeName, "java.sql") {
			continue
		}
		for _, root := range u.ops {
			if root.owner != op.owner || !gradedSurfaceConstructor(root) {
				continue
			}
			for i := 2; i < len(root.body)-1; i++ {
				if root.body[i].text == name && root.body[i-1].text == "." && root.body[i-2].text == "this" && root.body[i+1].text == "=" && i+2 < len(root.body) && root.parameterTypes[root.body[i+2].text] == f.typeName && statementEnd(root.body, i+2) == i+3 {
					owned[name] = true
				}
			}
		}
	}
	attempts := map[string]bool{}
	body := op.body
	for i, t := range body {
		if t.text != "try" || i+1 >= len(body) || body[i+1].text != "{" || !gradedUnconditional(body, i) {
			continue
		}
		close := matching(body, i+1, "{", "}")
		if close < 0 || close+2 >= len(body) || body[close+1].text != "catch" {
			continue
		}
		catchEnd := matching(body, close+2, "(", ")")
		if catchEnd < 0 || catchEnd+1 >= len(body) || body[catchEnd+1].text != "{" {
			continue
		}
		caught := gradedJDBCCatchType(u, body[close+3:catchEnd])
		if !caught {
			continue
		}
		end := matching(body, catchEnd+1, "{", "}")
		if end < 0 {
			continue
		}
		unsafe := false
		for _, tok := range body[catchEnd+2 : end] {
			unsafe = unsafe || tok.text == "return" || tok.text == "throw"
		}
		if unsafe {
			continue
		}
		for _, c := range callsIn(body[i+2 : close]) {
			if !strings.HasSuffix(c.name, ".close") || len(c.actuals) > 0 {
				continue
			}
			receiver := strings.TrimPrefix(strings.TrimSuffix(c.name, ".close"), "this.")
			if owned[receiver] && op.parameterTypes[receiver] == "" && gradedUnconditional(body[i+2:close], c.position) {
				attempts[receiver] = true
			}
		}
	}
	return len(attempts) >= 2
}

// A loop over an owned returned representation whose elements receive composed
// values establishes a connected possible projection duty. Getter/setter
// implementations may be absent, so retain this as an explicit estimate rather
// than claiming a proven mutation or library method contract.
func gradedPossibleOwnedProjection(roots []*operation, u unit) bool {
	returned := map[string]bool{}
	for _, op := range roots {
		for name := range gradedDirectReturnedFields(op, u) {
			returned[name] = true
		}
	}

	if len(returned) == 0 {
		return false
	}
	for _, op := range roots {
		body := pruneDeadFalseBranches(op.body)
		for i, t := range body {
			if t.text != "for" || i+1 >= len(body) || body[i+1].text != "(" {
				continue
			}
			end := matching(body, i+1, "(", ")")
			if end < 0 || end+1 >= len(body) || body[end+1].text != "{" {
				continue
			}
			colon := -1
			for j := i + 2; j < end; j++ {
				if body[j].text == ":" {
					colon = j
					break
				}
			}
			if colon <= i+2 {
				continue
			}
			element := body[colon-1].text
			start := colon + 1
			if start+2 < end && body[start].text == "this" && body[start+1].text == "." {
				start += 2
			}
			ownerConnected := start < end && returned[body[start].text] && gradeOwnedFieldReference(op, u, body, start)

			if !ownerConnected {
				continue
			}
			close := matching(body, end+1, "{", "}")
			if close < 0 {
				continue
			}
			segment := body[end+2 : close]
			invalid := false
			for j, t := range segment {
				if t.text == element && gradedStorageWriteAt(segment, j) {
					invalid = true
				}
			}
			if invalid {
				continue
			}
			for _, c := range callsIn(segment) {
				if !strings.HasPrefix(c.name, element+".") || len(c.actuals) == 0 {
					continue
				}
				for _, arg := range c.actuals {
					if len(callsIn(arg)) > 0 {
						return true
					}
				}
			}
		}
	}
	return false
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

func gradedJDBCCatchType(u unit, header []token) bool {
	// Remove the variable declarator, then resolve complete exception type names
	// in the union. Variable names never supply a type contract.
	if len(header) < 2 {
		return false
	}
	types := header[:len(header)-1]
	start := 0
	for end := 0; end <= len(types); end++ {
		if end < len(types) && types[end].text != "|" {
			continue
		}
		name := joinTokens(types[start:end])
		if name == "java.sql.SQLException" || name == "java.lang.Exception" || name == "java.lang.Throwable" {
			return true
		}
		if name == "SQLException" && gradedJavaLibraryType(u, name, "java.sql") || (name == "Exception" || name == "Throwable") && gradedJavaLibraryType(u, name, "java.lang") {
			return true
		}
		start = end + 1
	}
	return false
}

func gradedDirectReturnedFields(op *operation, u unit) map[string]bool {
	result := map[string]bool{}
	fields := callerDeclaredFields(u, op.owner)
	for _, statement := range gradedResultStatements(op.body) {
		if len(statement) < 2 || statement[0].text != "return" {
			continue
		}
		expression := statement[1:]
		arms := [][]token{expression}
		question, colon := -1, -1
		for i, t := range expression {
			if t.text == "?" {
				question = i
			}
			if t.text == ":" {
				colon = i
			}
		}
		if question >= 0 && colon > question {
			arms = [][]token{expression[question+1 : colon], expression[colon+1:]}
		}
		for _, arm := range arms {
			arm = trimSemicolonTokens(arm)
			for len(arm) > 1 && arm[0].text == "(" && matching(arm, 0, "(", ")") == len(arm)-1 {
				arm = arm[1 : len(arm)-1]
			}
			index := 0
			if len(arm) == 3 && arm[0].text == "this" && arm[1].text == "." {
				index = 2
			} else if len(arm) != 1 {
				continue
			}
			if _, ok := fields[arm[index].text]; ok && gradeOwnedFieldReference(op, u, arm, index) {
				result[arm[index].text] = true
			}
		}
	}
	return result
}
