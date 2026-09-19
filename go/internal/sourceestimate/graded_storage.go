package sourceestimate

// Storage effects classify each resolved write once. A computed owned write
// already contributes its invariant duty; only uncovered read/modify/write
// effects contribute local state. Independent writes remain independent.
type gradedStorageEffect struct {
	target          string
	computed, state bool
}

func gradeUncoveredStorageState(op *operation, u unit, body []token) bool {
	for _, effect := range gradedStorageEffects(op, u, body) {
		if effect.state && !effect.computed {
			return true
		}
	}
	return false
}

// Track a simple, unreassigned alias to the enclosing instance. Ambiguous
// assignments invalidate the alias rather than silently reusing its identity.

func gradedScopeContains(body []token, declaration, reference int) bool {
	stack := []int{}
	for i := 0; i < declaration; i++ {
		if body[i].text == "{" {
			stack = append(stack, i)
		}
		if body[i].text == "}" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) == 0 {
		return true
	}
	close := matching(body, stack[len(stack)-1], "{", "}")
	return close >= reference
}
func gradedStorageReferenceStart(body []token, field int) int {
	if field < 2 || body[field-1].text != "." {
		return field
	}
	if body[field-2].text != ")" {
		return field - 2
	}
	for i := field - 3; i >= 0; i-- {
		if body[i].text == "(" && matching(body, i, "(", ")") == field-2 {
			return i
		}
	}
	return -1
}
func gradedUnitStep(op *operation, u unit, body []token, operator, target int) bool {
	switch body[operator].text {
	case "++", "--":
		return true
	case "+=", "-=":
		end := statementEnd(body, operator+1)
		return end == operator+2 && body[operator+1].text == "1"
	case "=":
		end := statementEnd(body, operator+1)
		if end < operator+4 || body[end-1].text != "1" || body[end-2].text != "+" && body[end-2].text != "-" {
			return false
		}
		field := end - 3
		return body[field].text == body[target].text && gradedStorageReferenceStart(body, field) == operator+1 && gradeOwnedFieldReference(op, u, body, field)
	}
	return false
}

// Go's panic is a builtin only while it is not shadowed by a parameter, local,
// field receiver or same-file declaration. Unresolved overrides earn no proof.
func gradedBuiltinPanic(op *operation, u unit) bool {
	if op.language != "go" {
		return false
	}
	for _, name := range op.paramNames {
		if name == "panic" {
			return false
		}
	}
	for _, candidate := range u.ops {
		if candidate.name == "panic" {
			return false
		}
	}
	for i, tok := range op.body {
		if tok.text == "panic" && i+1 < len(op.body) && (op.body[i+1].text == ":=" || op.body[i+1].text == "=") {
			return false
		}
	}
	return true
}

func onlyStorageSnapshots(items []evidenceItem) bool {
	for _, item := range items {
		if item.category != "storage_snapshot" {
			return false
		}
	}
	return true
}

// Return the storage references on the left of an assignment, walking every
// comma-separated target without mistaking RHS commas or index operands for
// additional targets. Locations remain relative to the original token slice.
