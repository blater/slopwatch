package sourceestimate

func gradeOwnerAlias(op *operation, body []token, name string, before int) bool {
	alias := false
	for i := 0; i+2 < before; i++ {
		if body[i].text != name || body[i+1].text != "=" && body[i+1].text != ":=" {
			continue
		}
		if i > 0 && body[i-1].text == "." {
			continue
		}
		// Resolve the binding written here. A declaration in a completed
		// block belongs to its inner local; assigning an outer local inside
		// that block still changes the binding used after the block.
		declaration := -1
		for j := 0; j <= i; j++ {
			if body[j].text == name && gradedAliasDeclaration(body, j) && gradedScopeContains(body, j, i) {
				declaration = j
			}
		}
		if declaration >= 0 && !gradedScopeContains(body, declaration, before) {
			continue
		}
		rhs := body[i+2].text
		terminal := i+3 >= len(body) || body[i+3].text == ";" || op.language == "go" && isIdentifier(body[i+3].text)
		alias = operationBodyUnconditional(op, body, i) && (rhs == "this" || rhs == "self" || rhs == op.receiverName) && terminal
	}
	return alias
}
