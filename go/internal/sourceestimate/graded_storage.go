package sourceestimate

// Storage effects classify each resolved write once. A computed owned write
// already contributes its invariant duty; only uncovered read/modify/write
// effects contribute local state. Independent writes remain independent.
type gradedStorageEffect struct {
	target          string
	computed, state bool
}

func gradedStorageEffects(op *operation, u unit, body []token) []gradedStorageEffect {
	fields := callerDeclaredFields(u, op.owner)
	result := []gradedStorageEffect{}
	for i, tok := range body {
		operator := tok.text
		if operator != "=" && operator != "+=" && operator != "-=" && operator != "*=" && operator != "/=" && operator != "%=" && operator != "++" && operator != "--" {
			continue
		}
		target := i - 1
		if (operator == "++" || operator == "--") && i+1 < len(body) && isIdentifier(body[i+1].text) {
			target = i + 1
			if isMember(body, target) {
				target += 2
			}
		}
		if target < 0 || target >= len(body) || !isIdentifier(body[target].text) {
			continue
		}
		_, declared := fields[body[target].text]
		owned := declared && gradeOwnedFieldReference(op, u, body, target)
		if owned && op.language == "go" && !gradedMutableGoReceiver(op, u) {
			continue
		}
		member := target >= 2 && body[target-1].text == "."
		if !owned && !member {
			continue
		}
		rhs := body[i+1 : statementEnd(body, i+1)]
		state := operator == "++" || operator == "--"
		computed := false
		if operator != "=" && !state {
			state = compoundUpdateChanges(rhs, operator)
			computed = owned && state
		} else if operator == "=" {
			computed = owned && hasTransform(rhs)
			if member {
				state = readsAndChangesMember(body[target-2:target+1], rhs)
			}
			if owned && hasTransform(rhs) {
				for j, t := range rhs {
					if t.text == body[target].text && gradeOwnedFieldReference(op, u, body, i+1+j) {
						state = true
					}
				}
			}
		}
		// Unit increments have the same state duty in postfix, compound and
		// expanded spelling, including when another computed field is written.
		if owned && state && gradedUnitStep(op, u, body, i, target) {
			computed = false
		}
		name := body[target].text
		if !owned && member {
			name = joinTokens(body[target-2 : target+1])
		}
		result = append(result, gradedStorageEffect{target: name, computed: computed, state: state})
	}
	return result
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
		alias = gradedUnconditional(body, i) && (rhs == "this" || rhs == "self" || rhs == op.receiverName) && terminal
	}
	return alias
}

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

func gradedAliasDeclaration(body []token, index int) bool {
	if index+1 < len(body) && body[index+1].text == ":=" {
		return true
	}
	if index == 0 {
		return false
	}
	previous := body[index-1].text
	if previous == "let" || previous == "const" || previous == "var" {
		return true
	}
	return isIdentifier(previous) && previous != "return" && previous != "throw" && previous != "yield" && previous != "else"
}

// Return the storage references on the left of an assignment, walking every
// comma-separated target without mistaking RHS commas or index operands for
// additional targets. Locations remain relative to the original token slice.
func gradedAssignmentTargets(body []token, operator int) []int {
	targets := []int{}
	for end := operator - 1; end >= 0; {
		target, start := end, end
		if body[end].text == "]" {
			depth := 1
			open := end - 1
			for open >= 0 {
				if body[open].text == "]" {
					depth++
				}
				if body[open].text == "[" {
					depth--
					if depth == 0 {
						break
					}
				}
				open--
			}
			if open < 1 {
				break
			}
			target = open - 1
			start = gradedStorageReferenceStart(body, target)
		} else if body[end].text == ")" {
			depth := 1
			open := end - 1
			for open >= 0 {
				if body[open].text == ")" {
					depth++
				}
				if body[open].text == "(" {
					depth--
					if depth == 0 {
						break
					}
				}
				open--
			}
			if open < 0 {
				break
			}
			target = open + 1
			if target < end && body[target].text == "*" {
				target++
			}
			if target+1 != end {
				break
			}
			start = open
		} else {
			if !isIdentifier(body[target].text) {
				break
			}
			start = gradedStorageReferenceStart(body, target)
		}
		if start < 0 || !isIdentifier(body[target].text) {
			break
		}
		if start > 0 && body[start-1].text == "*" {
			start--
		}
		targets = append(targets, target)
		if start == 0 || body[start-1].text != "," {
			break
		}
		end = start - 2
	}
	return targets
}
