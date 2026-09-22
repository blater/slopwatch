package sourceestimate

import "strings"

func gradeOwnedOperations(root *operation, units []unit, byKey *operationLookup) []*operation {
	result := []*operation{}
	seen := map[string]bool{}
	var visit func(*operation, int)
	visit = func(op *operation, depth int) {
		if depth > maxCallDepth || len(result) >= maxCallsPerRoot || seen[op.id] {
			return
		}
		seen[op.id] = true
		result = append(result, op)
		for _, c := range callsIn(normalizedPrunedBody(op)) {
			matches := resolveCall(op, c, units, byKey)
			if matches.count() == 1 && matches.unique().owner == root.owner && !matches.unique().exposed {
				visit(matches.unique(), depth+1)
			}
		}
	}
	visit(root, 0)
	return result
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func gradeOwnedReceiver(op *operation, u unit, receiver string) bool {
	parts := strings.Split(receiver, ".")
	fields := callerDeclaredFields(u, op.owner)
	for _, part := range parts {
		if _, exists := fields[part]; exists {
			return true
		}
	}
	return false
}
func gradeCommand(op *operation) bool {
	for _, tok := range op.body {
		if tok.text == "return" {
			return false
		}
	}
	// Rust's result type is recorded separately by its inventory.
	if op.language == "rust" && op.returnType != "" && op.returnType != "()" {
		return false
	}
	return true
}
func gradeReceiver(op *operation, receiver string) string {
	// A single borrowed constructor argument retains the owned receiver's
	// identity through a local RAII guard, including tuple-field access.
	dot := strings.IndexByte(receiver, '.')
	local := receiver
	if dot >= 0 {
		local = receiver[:dot]
	}
	for i := 0; i+3 < len(op.body); i++ {
		if op.body[i].text != local || op.body[i+1].text != "=" || op.body[i+3].text != "(" {
			continue
		}
		end := matching(op.body, i+3, "(", ")")
		if end < 0 {
			continue
		}
		arg := op.body[i+4 : end]
		if len(arg) > 0 && arg[0].text == "&" {
			arg = arg[1:]
			if len(arg) > 0 && arg[0].text == "mut" {
				arg = arg[1:]
			}
		}
		if len(arg) == 3 && (arg[0].text == "self" || arg[0].text == op.receiverName) && arg[1].text == "." {
			return arg[0].text + "." + arg[2].text
		}
	}
	return receiver
}
func gradedMutableGoReceiver(op *operation, u unit) bool {
	if op.language != "go" {
		return true
	}
	var source *token
	if len(u.tokens) > 0 {
		source = &u.tokens[0]
	}
	if u.inventory == nil {
		return indexGoReceivers(u.tokens)[goReceiverKey{op.name, op.owner}]
	}
	cached := u.inventory.goReceivers
	if cached == nil || cached.source != source || cached.length != len(u.tokens) {
		cached = &goReceiverInventory{source: source, length: len(u.tokens), receivers: indexGoReceivers(u.tokens)}
		u.inventory.goReceivers = cached
	}
	return cached.receivers[goReceiverKey{op.name, op.owner}]
}

type goReceiverInventory struct {
	source    *token
	length    int
	receivers map[goReceiverKey]bool
}

// Preserve the scanner's first matching declaration, including every receiver
// token as an owner candidate rather than inferring owner from parsed operations.
type goReceiverKey struct{ name, owner string }

func indexGoReceivers(tokens []token) map[goReceiverKey]bool {
	result := map[goReceiverKey]bool{}
	for i, t := range tokens {
		if t.text != "func" || i+1 >= len(tokens) || tokens[i+1].text != "(" {
			continue
		}
		close := matching(tokens, i+1, "(", ")")
		if close < 0 || close+1 >= len(tokens) {
			continue
		}
		receiver := tokens[i+2 : close]
		pointer := false
		for _, tok := range receiver {
			pointer = pointer || tok.text == "*"
		}
		for _, tok := range receiver {
			key := goReceiverKey{tokens[close+1].text, tok.text}
			if _, found := result[key]; !found {
				result[key] = pointer
			}
		}
	}
	return result
}
