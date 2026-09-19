package sourceestimate

// gradedOwnedQuery admits a sole read-only query through a field declared by
// the owner (for example `return graph.child(edge)`). The declaration and
// field type checks keep package/static utility calls and private helper calls
// from being mistaken for cheap accessors.
func gradedOwnedQuery(u unit, op *operation, body []token) bool {
	if op == nil || op.owner == "" || len(body) < 5 || body[0].text != "return" || gradedBodyHasControl(body) || gradedHasAssignment(body) {
		return false
	}
	open := -1
	for i := 1; i < len(body); i++ {
		if body[i].text == "(" {
			open = i
			break
		}
		if body[i].text == ";" {
			return false
		}
	}
	if open < 3 {
		return false
	}
	close := matching(body, open, "(", ")")
	if close < 0 || close != len(body)-1 || body[open-1].text == "(" {
		return false
	}
	for _, item := range body[open+1 : close] {
		if item.text == "(" || item.text == ")" || item.text == "+" || item.text == "-" || item.text == "*" || item.text == "/" {
			return false
		}
	}
	fields := callerDeclaredFields(u, op.owner)
	if len(fields) == 0 {
		return false
	}
	// The method name is immediately before the call's opening parenthesis.
	// A declared field immediately before a member dot is the receiver root.
	for i := 1; i+1 < open-1; i++ {
		fieldName := body[i].text
		field, declared := fields[fieldName]
		if !declared {
			continue
		}
		if body[i+1].text != "." {
			continue
		}
		if _, typed := op.fieldTypes[fieldName]; !typed && field.typeName == "" {
			continue
		}
		for _, prefix := range body[1:i] {
			if !isIdentifier(prefix.text) && prefix.text != "." {
				return false
			}
		}
		return true
	}
	return false
}
