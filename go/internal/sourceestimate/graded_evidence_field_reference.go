package sourceestimate

func gradeOwnedFieldReference(op *operation, u unit, body []token, index int) bool {
	if index < 0 || index >= len(body) {
		return false
	}
	if op.constraintReceiver != "" {
		return index >= 2 && body[index-1].text == "." && body[index-2].text == op.constraintReceiver && (index < 4 || body[index-3].text != "." || body[index-4].text == "this")
	}
	name := body[index].text
	// Callers have already checked their single field inventory. Do not parse
	// the containing type again for each candidate token.
	if index >= 2 && body[index-1].text == "." {
		receiverIndex := index - 2
		if body[receiverIndex].text == ")" {
			start := gradedStorageReferenceStart(body, index)
			if start < 0 || body[start].text != "(" {
				return false
			}
			receiverIndex = start + 1
			if receiverIndex < len(body) && body[receiverIndex].text == "*" {
				receiverIndex++
			}
			if receiverIndex+1 != index-2 {
				return false
			}
		}
		receiver := body[receiverIndex].text
		return receiver == "this" || receiver == "self" || receiver == op.receiverName || gradeOwnerAlias(op, body, receiver, receiverIndex)
	}
	if op.language != "java" {
		return false
	}
	for _, p := range op.paramNames {
		if p == name {
			return false
		}
	}
	for i := 1; i <= index; i++ {
		if body[i].text != name || !gradedScopeContains(body, i, index) {
			continue
		}
		previous := body[i-1].text
		if previous == "]" && i >= 3 && body[i-2].text == "[" {
			previous = body[i-3].text
		}
		if previous == "let" || previous == "var" || previous == "const" {
			return false
		}
		if i+1 < len(body) && isIdentifier(previous) && previous != "return" && previous != "throw" {
			next := body[i+1].text
			if next == "=" || next == ";" || next == ":" {
				return false
			}
		}
	}
	return true
}
