package sourceestimate

import "strings"

// gradeLocalCoordination recognizes coordination that is visible in one
// operation body. The receiver must be an owner-declared delegate. Two
// unrelated reads do not establish coordination: the body must contain a
// command followed by a returned result, or pass one delegate result into a
// later delegate call.
func gradeLocalCoordination(op *operation, u unit, body []token, receiver string) bool {
	if op == nil || op.owner == "" || receiver == "" || len(body) == 0 {
		return false
	}
	if !gradedCoordinationOwnedReceiver(op, u, receiver) {
		return false
	}
	calls := gradedReceiverCalls(op, body, receiver)
	if len(calls) < 2 {
		return false
	}
	seen := map[string]bool{}
	command, result := false, false
	produced := map[string]bool{}
	for _, call := range calls {
		seen[call.name] = true
		returned := gradedCallReturns(body, call) || gradedTailResult(op, body, call)
		if returned {
			result = true
		}
		if !returned && gradedCallAssignedName(body, call) == "" {
			command = true
		}
		if name := gradedCallAssignedName(body, call); name != "" {
			produced[name] = true
		}
	}
	if len(seen) < 2 {
		return false
	}
	if command && result {
		return true
	}
	for _, call := range calls {
		for _, actual := range call.actuals {
			for _, item := range actual {
				if produced[item.text] {
					return true
				}
			}
		}
	}
	return false
}

// gradeGuaranteedCleanup requires a language cleanup construct to schedule an
// actual call associated with an owner-declared delegate or a Rust RAII guard.
// Tokens such as `finally`, `defer`, or `drop` alone are not evidence.
func gradeGuaranteedCleanup(ops []*operation, units []unit, byKey map[string][]*operation) bool {
	return gradedCleanupMode(ops, units, byKey, false)
}

func gradedCleanupMode(ops []*operation, units []unit, byKey map[string][]*operation, acquisition bool) bool {
	if len(ops) == 0 || len(units) == 0 {
		return false
	}
	for _, op := range ops {
		if op == nil || op.file < 0 || op.file >= len(units) {
			continue
		}
		u := units[op.file]
		body := pruneDeadFalseBranches(op.body)
		if gradedFinallyCleanup(op, u, units, body, byKey, acquisition) || gradedDeferCleanup(op, u, units, body, byKey, acquisition) || gradedRustRAIICleanup(op, u, body, units, byKey, acquisition) {
			return true
		}
	}
	return false
}

func gradedFinallyCleanup(op *operation, u unit, units []unit, body []token, byKey map[string][]*operation, acquisition bool) bool {
	for i, item := range body {
		if item.text != "finally" {
			continue
		}
		start, end := i+1, len(body)
		if start < len(body) && body[start].text == "{" {
			close := matching(body, start, "{", "}")
			if close < 0 {
				continue
			}
			start, end = start+1, close
		}
		protectedStart, protectedEnd := gradedTryProtected(body, i)
		if protectedStart < 0 {
			continue
		}
		if gradedCleanupCallInRange(op, u, units, body, start, end, protectedStart, protectedEnd, byKey, acquisition) {
			return true
		}
	}
	return false
}

func gradedDeferCleanup(op *operation, u unit, units []unit, body []token, byKey map[string][]*operation, acquisition bool) bool {
	for i, item := range body {
		if item.text != "defer" || !gradedUnconditional(body, i) {
			continue
		}
		end := i + 1
		for end < len(body) && body[end].text != "(" && body[end].text != ";" {
			end++
		}
		if end >= len(body) || body[end].text != "(" {
			continue
		}
		end = matching(body, end, "(", ")")
		if end < 0 {
			continue
		}
		// A deferred closure includes its body; a deferred call ends at its
		// argument list. Later statements are never part of either cleanup.
		if i+1 < len(body) && body[i+1].text == "func" && end+1 < len(body) && body[end+1].text == "{" {
			end = matching(body, end+1, "{", "}")
			if end < 0 {
				continue
			}
		}
		if gradedCleanupCallInRange(op, u, units, body, i+1, end+1, end+1, len(body), byKey, acquisition) {
			return true
		}
	}
	return false
}

func gradedCleanupCallInRange(op *operation, u unit, units []unit, body []token, start, end, protectedStart, protectedEnd int, byKey map[string][]*operation, acquisition bool) bool {
	if start < 0 || start >= end || end > len(body) {
		return false
	}
	segment := body[start:end]
	// A conditional cleanup call is not guaranteed on the relevant exit paths.
	for _, tok := range segment {
		if tok.text == "if" || tok.text == "else" || tok.text == "for" || tok.text == "while" || tok.text == "loop" || tok.text == "switch" || tok.text == "?" || tok.text == "&&" || tok.text == "||" {
			return false
		}
	}
	for _, call := range callsIn(segment) {
		dot := strings.LastIndexByte(call.name, '.')
		if dot >= 0 {
			receiver := call.name[:dot]
			if gradedCoordinationOwnedReceiver(op, u, receiver) && gradedCleanupResolvedCall(op, u, units, call, byKey) && gradedConnectedCleanup(op, u, units, body, start, end, protectedStart, protectedEnd, call, byKey, acquisition) {
				return true
			}
		}
	}
	return false
}

func gradedCleanupCandidates(op *operation, u unit, units []unit, c call, byKey map[string][]*operation) []*operation {
	matches := resolveCall(op, c, units, byKey)
	if len(matches) == 0 {
		dot := strings.LastIndexByte(c.name, '.')
		if dot >= 0 {
			fields := callerDeclaredFields(u, op.owner)
			for _, part := range strings.Split(c.name[:dot], ".") {
				if field, ok := fields[part]; ok && field.typeName != "" {
					matches = byKey[scopedOperationKey(op, c.name[dot+1:], field.typeName)]
					break
				}
			}
		}
	}
	// Follow a bounded unconditional forwarding helper, keeping the resolved
	// owner/storage identity rather than granting credit to the helper name.
	for depth := 0; len(matches) == 1 && depth < maxCallDepth; depth++ {
		candidate := matches[0]
		calls := callsIn(candidate.body)
		if len(calls) != 1 || gradedHasAssignment(candidate.body) || !gradedUnconditional(candidate.body, calls[0].position) {
			break
		}
		if len(gradedBooleanWrites(candidate, units, "true")) > 0 || len(gradedBooleanWrites(candidate, units, "false")) > 0 {
			break
		}
		next := resolveCall(candidate, calls[0], units, byKey)
		if len(next) != 1 || next[0].owner != candidate.owner || next[0] == candidate {
			break
		}
		matches = next
	}
	return matches
}
func gradedCleanupResolvedCall(op *operation, u unit, units []unit, c call, byKey map[string][]*operation) bool {
	candidates := gradedCleanupCandidates(op, u, units, c, byKey)
	return len(candidates) == 1 && len(gradedCleanupResetFields(candidates[0], units)) > 0
}

// A reset of an owned boolean protocol field is a bounded, spelling-independent
// cleanup witness. Arbitrary mutations (e.g. logging counters) are not cleanup.
func gradedCleanupResetFields(op *operation, units []unit) map[string]string {
	result := map[string]string{}
	if op == nil || op.file < 0 || op.file >= len(units) {
		return result
	}
	for _, state := range []string{"false", "true"} {
		for _, field := range gradedBooleanWrites(op, units, state) {
			result[field] = state
		}
	}
	return result
}

func gradedConnectedCleanup(op *operation, u unit, units []unit, body []token, start, end, protectedStart, protectedEnd int, cleanup call, byKey map[string][]*operation, acquisition bool) bool {
	candidates := gradedCleanupCandidates(op, u, units, cleanup, byKey)
	if len(candidates) != 1 {
		return false
	}
	reset := gradedCleanupResetFields(candidates[0], units)
	receiver := cleanup.name[:strings.LastIndexByte(cleanup.name, '.')]
	acquired, used := map[string]bool{}, map[string]bool{}
	invalidated := map[string]bool{}
	if gradedReceiverReplaced(body, 0, end, receiver) {
		return false
	}
	for _, c := range callsIn(body) {
		if c.position >= start && c.position < end || unreachableCall(body, c) {
			continue
		}
		dot := strings.LastIndexByte(c.name, '.')
		if dot < 0 || gradeReceiver(op, c.name[:dot]) != gradeReceiver(op, receiver) {
			continue
		}
		matches := gradedCleanupCandidates(op, u, units, c, byKey)
		if len(matches) != 1 {
			for field := range reset {
				acquired[field], invalidated[field] = false, true
			}
			continue
		}
		other := matches[0]
		if other.owner != candidates[0].owner || other.pkg != candidates[0].pkg {
			continue
		}
		for i, tok := range other.body {
			if reset[tok.text] == "" || !gradeOwnedFieldReference(other, units[other.file], other.body, i) {
				continue
			}
			// Acquisition changes the same storage to the opposite cleanup state. The latter
			// supports cleanup owned by a complete() stage after caller acquisition.
			if gradedStorageWriteAt(other.body, i) {
				acquired[tok.text], invalidated[tok.text] = false, true
				if gradedHasBooleanWrite(other, units, tok.text, gradedOppositeBoolean(reset[tok.text])) && c.position < start && gradedUnconditional(body, c.position) && gradedUnconditional(other.body, i) && !gradedReceiverReplaced(body, c.position, start, receiver) {
					acquired[tok.text], invalidated[tok.text] = true, false
					for _, tok := range body[c.position:maxInt(c.position, protectedStart)] {
						if tok.text == "return" || tok.text == "throw" {
							return false
						}
					}
				}
			} else if !gradedReceiverReplaced(body, minInt(c.position, start), maxInt(c.position, end), receiver) {
				if invalidated[tok.text] || acquisition && !acquired[tok.text] || !gradedProtocolReadAdmits(other, units[other.file], i, reset[tok.text]) {
					return false
				}
				if c.position < protectedStart || c.position >= protectedEnd {
					return false
				}
				used[tok.text] = true
			}
		}
	}
	for field := range reset {
		if acquired[field] && used[field] || !acquisition && (used[field] || acquired[field]) {
			return true
		}
	}
	return false
}

func gradedRustRAIICleanup(op *operation, u unit, body []token, units []unit, byKey map[string][]*operation, acquisition bool) bool {
	if op.language != "rust" {
		return false
	}
	fields := callerDeclaredFields(u, op.owner)
	for i := 0; i+5 < len(body); i++ {
		if body[i].text != "let" {
			continue
		}
		n := i + 1
		if body[n].text == "mut" {
			n++
		}
		if n+3 >= len(body) || body[n+1].text != "=" || body[n+3].text != "(" {
			continue
		}
		close := matching(body, n+3, "(", ")")
		if close < 0 {
			continue
		}
		arg := body[n+4 : close]
		if len(arg) > 0 && arg[0].text == "&" {
			arg = arg[1:]
		}
		if len(arg) > 0 && arg[0].text == "mut" {
			arg = arg[1:]
		}
		if len(arg) != 3 || arg[0].text != "self" || arg[1].text != "." {
			continue
		}
		field, ok := fields[arg[2].text]
		if !ok || field.typeName == "" {
			continue
		}
		guard, guardType := body[n].text, body[n+2].text
		escaped := false
		for j := close + 1; j < len(body); j++ {
			if body[j].text != guard {
				continue
			}
			projected := j+1 < len(body) && body[j+1].text == "."
			dropped := j >= 2 && j+1 < len(body) && body[j-2].text == "drop" && body[j-1].text == "(" && body[j+1].text == ")"
			if !projected && !dropped {
				escaped = true
			}
		}
		for _, c := range callsIn(body[close+1:]) {
			for _, actual := range c.actuals {
				if len(actual) == 1 && actual[0].text == guard && c.name != "drop" {
					escaped = true
				}
			}
		}
		if escaped {
			continue
		}
		for _, function := range rustFunctions(u.tokens) {
			if function.name != "drop" || function.owner != guardType || function.impl == nil || function.impl.trait != "Drop" {
				continue
			}
			dropBody := u.tokens[function.bodyStart+1 : function.bodyEnd]
			for _, c := range callsIn(dropBody) {
				if !strings.HasPrefix(c.name, "self.0.") || !gradedUnconditional(dropBody, c.position) {
					continue
				}
				method := strings.TrimPrefix(c.name, "self.0.")
				for _, resourceUnit := range units {
					for _, release := range rustFunctions(resourceUnit.tokens) {
						if release.owner != field.typeName || release.name != method {
							continue
						}
						reset := gradedRustCleanupReset(release, resourceUnit, 0)
						if len(reset) > 0 {
							receiver := "self." + arg[2].text
							acquired, used := map[string]bool{}, map[string]bool{}
							invalidated := map[string]bool{}
							guardCalls := false
							for _, actual := range callsIn(body) {
								dot := strings.LastIndexByte(actual.name, '.')
								if dot < 0 {
									continue
								}
								if actual.name[:dot] == receiver && actual.position < i {
									for _, resolved := range gradedCleanupCandidates(op, u, units, actual, byKey) {
										for effect, state := range reset {
											if gradedOperationWritesField(resolved, units[resolved.file], effect) {
												acquired[effect] = gradedHasBooleanWrite(resolved, units, effect, gradedOppositeBoolean(state)) && gradedUnconditional(body, actual.position)
												invalidated[effect] = !acquired[effect]
											}
										}
									}
								}
								if strings.HasPrefix(actual.name, guard+".0.") && actual.position > close {
									guardCalls = true
									connected := actual
									connected.name = receiver + "." + actual.name[strings.LastIndexByte(actual.name, '.')+1:]
									matches := gradedCleanupCandidates(op, u, units, connected, byKey)
									for field := range reset {
										if len(matches) != 1 || gradedOperationWritesField(matches[0], units[matches[0].file], field) {
											acquired[field], invalidated[field] = false, true
										}
									}
									for field, state := range reset {
										used[field] = used[field] || !invalidated[field] && (!acquisition || acquired[field]) && gradedCallObservesProtocol(op, u, units, connected, byKey, map[string]string{field: state})
									}
								}
								for _, argument := range actual.actuals {
									if joinTokens(argument) == guard+".0" {
										guardCalls = true
									}
								}
							}
							for field := range reset {
								if acquired[field] && used[field] || !acquisition && (!guardCalls || used[field]) {
									return true
								}
							}
						}
					}
				}
			}
		}
	}
	return false
}

func gradedReceiverCalls(op *operation, body []token, receiver string) []call {
	result := []call{}
	prefix := receiver + "."
	for _, call := range callsIn(body) {
		if strings.HasPrefix(call.name, prefix) {
			result = append(result, call)
			continue
		}
		if dot := strings.LastIndexByte(call.name, '.'); dot >= 0 && gradeReceiver(op, call.name[:dot]) == receiver {
			result = append(result, call)
		}
	}
	return result
}

func gradedCallReturns(body []token, candidate call) bool {
	start := candidate.position - 1
	for start >= 0 && body[start].text != ";" && body[start].text != "{" && body[start].text != "}" {
		start--
	}
	for _, item := range body[start+1 : candidate.position] {
		if item.text == "return" {
			return true
		}
	}
	return false
}

func gradedTailResult(op *operation, body []token, candidate call) bool {
	assigned := gradedCallAssignedName(body, candidate)
	if assigned != "" {
		for i := candidate.position + 1; i+1 < len(body); i++ {
			if body[i].text == "return" && body[i+1].text == assigned {
				return true
			}
		}
	}
	if op.language != "rust" || op.returnType == "" || op.returnType == "()" || len(body) == 0 {
		return false
	}
	if assigned != "" {
		return body[len(body)-1].text == assigned
	}
	open := candidate.position + 1
	if open < len(body) && body[open].text == "(" {
		return matching(body, open, "(", ")") == len(body)-1
	}
	return false
}

func gradedCallAssignedName(body []token, candidate call) string {
	start := candidate.position - 1
	for start >= 0 && body[start].text != ";" && body[start].text != "{" && body[start].text != "}" {
		start--
	}
	for i := start + 1; i+1 < candidate.position; i++ {
		if body[i].text != "=" && body[i].text != ":=" {
			continue
		}
		if i > start+1 && isIdentifier(body[i-1].text) {
			return body[i-1].text
		}
	}
	return ""
}

func gradedCoordinationOwnedReceiver(op *operation, u unit, receiver string) bool {
	if op == nil || op.owner == "" || receiver == "" {
		return false
	}
	fields := callerDeclaredFields(u, op.owner)
	if len(fields) == 0 {
		return false
	}
	parts := strings.Split(receiver, ".")
	for i, part := range parts {
		if part == "this" || part == "self" || part == op.receiverName {
			continue
		}
		field, ok := fields[part]
		if !ok {
			continue
		}
		if _, typed := op.fieldTypes[part]; typed || field.typeName != "" {
			return true
		}
		if i+1 < len(parts) && parts[i+1] == "0" {
			return true
		}
	}
	return false
}

// Missing implementations retain a bounded resource envelope when an owned
// delegate is used outside cleanup and scheduled inside it. This is expressly
// estimated: the calls alone do not prove a release or acquisition contract.
func gradedUnresolvedCleanup(roots []*operation, units []unit, byKey map[string][]*operation) bool {
	for _, root := range roots {
		for _, op := range gradeOwnedOperations(root, units, byKey) {
			body := pruneDeadFalseBranches(op.body)
			for i, tok := range body {
				start, end, protectedStart, protectedEnd := -1, -1, -1, -1
				if tok.text == "finally" && i+1 < len(body) && body[i+1].text == "{" {
					start, end = i+2, matching(body, i+1, "{", "}")
					protectedStart, protectedEnd = gradedTryProtected(body, i)
				}
				if tok.text == "defer" && gradedUnconditional(body, i) {
					start = i + 1
					open := start
					for open < len(body) && body[open].text != "(" && body[open].text != ";" {
						open++
					}
					if open < len(body) && body[open].text == "(" {
						end = matching(body, open, "(", ")") + 1
					}
					protectedStart, protectedEnd = end, len(body)
				}
				if start < 0 || end <= start || protectedStart < 0 {
					continue
				}
				segment := body[start:end]
				for _, c := range callsIn(segment) {
					dot := strings.LastIndexByte(c.name, '.')
					if dot < 0 || !gradedUnconditional(segment, c.position) {
						continue
					}
					receiver := c.name[:dot]
					if !gradedCoordinationOwnedReceiver(op, units[op.file], receiver) || len(gradedCleanupCandidates(op, units[op.file], units, c, byKey)) != 0 || gradedReceiverReplaced(body, 0, len(body), receiver) {
						continue
					}
					protected, bad := false, false
					for _, use := range callsIn(body) {
						udot := strings.LastIndexByte(use.name, '.')
						if udot < 0 || use.name[:udot] != receiver || use.position >= start && use.position < end || unreachableCall(body, use) {
							continue
						}
						if use.position >= protectedStart && use.position < protectedEnd {
							protected = true
						} else if use.position >= protectedEnd || gradedCallReturns(body, use) {
							bad = true
						}
					}
					if protected && !bad {
						return true
					}
				}
			}
		}
	}
	return false
}

// Reject effects nested inside a conditional/loop, including unbraced bodies.
// Admission guards that exit before a later unconditional write remain valid.
func gradedUnconditional(body []token, position int) bool {

	depth := 0
	for i := 0; i < position; i++ {
		if body[i].text == "{" {
			depth++
		}
		if body[i].text == "}" {
			depth--
		}
		if depth == 0 && (body[i].text == "return" || body[i].text == "throw" || body[i].text == "panic" && i+1 < len(body) && body[i+1].text == "(") && statementEnd(body, i) < position && gradedUnconditional(body, i) {
			return false
		}
	}
	for i := 0; i < position; i++ {
		switch body[i].text {
		case "if", "else", "for", "while", "loop", "switch", "match":
		default:
			continue
		}
		start := i + 1
		if start < len(body) && body[start].text == "(" {
			end := matching(body, start, "(", ")")
			if end < 0 {
				return false
			}
			start = end + 1
		} else if body[i].text != "else" {
			for start < len(body) && body[start].text != "{" {
				start++
			}
		}
		if start >= len(body) {
			continue
		}
		end := statementEnd(body, start)
		if body[start].text == "{" {
			end = matching(body, start, "{", "}")
		}
		if position >= start && position <= end {
			return false
		}
	}
	return true
}
func gradedReceiverReplaced(body []token, start, end int, receiver string) bool {
	parts := strings.Split(receiver, ".")
	for i := start; i+1 < end && i+1 < len(body); i++ {
		if body[i].text == parts[len(parts)-1] && (body[i+1].text == "=" || body[i+1].text == ":=") {
			return true
		}
	}
	return false
}

func gradedBooleanWrites(op *operation, units []unit, value string) []string {
	fields := callerDeclaredFields(units[op.file], op.owner)
	final := map[string]bool{}
	lastWrite := map[string]int{}
	for i := 0; i+1 < len(op.body); i++ {
		if _, ok := fields[op.body[i].text]; !ok || !gradeOwnedFieldReference(op, units[op.file], op.body, i) {
			continue
		}
		if !gradedStorageWriteAt(op.body, i) {
			continue
		}
		lastWrite[op.body[i].text] = i
		final[op.body[i].text] = gradedExactBooleanAssignment(op.body, i, value) && gradedUnconditional(op.body, i)
	}
	// A later call may restore or otherwise mutate the same protocol state.
	// Without a complete call effect contract, the earlier literal is not a
	// final-value proof (including when that call resolves to reacquisition).
	for _, c := range callsIn(op.body) {
		for field, position := range lastWrite {
			if c.position > position {
				final[field] = false
			}
		}
	}
	result := []string{}
	for name, valid := range final {
		if valid {
			result = append(result, name)
		}
	}
	return result
}
func gradedExactBooleanAssignment(body []token, index int, value string) bool {
	if index+2 >= len(body) || body[index+1].text != "=" {
		return false
	}
	end := statementEnd(body, index+2)
	return end == index+3 && body[index+2].text == value
}

func gradedRustCleanupReset(function rustFunction, u unit, depth int) map[string]string {
	if depth >= maxCallDepth {
		return nil
	}
	candidate := &operation{language: "rust", owner: function.owner, file: 0, body: u.tokens[function.bodyStart+1 : function.bodyEnd]}
	reset := gradedCleanupResetFields(candidate, []unit{u})
	if len(reset) > 0 {
		return reset
	}
	calls := callsIn(candidate.body)
	if len(calls) != 1 || gradedHasAssignment(candidate.body) || !strings.HasPrefix(calls[0].name, "self.") || !gradedUnconditional(candidate.body, calls[0].position) {
		return nil
	}
	name := strings.TrimPrefix(calls[0].name, "self.")
	for _, helper := range rustFunctions(u.tokens) {
		if helper.owner == function.owner && helper.name == name {
			return gradedRustCleanupReset(helper, u, depth+1)
		}
	}
	return nil
}

// A protocol read must affect an outcome, admission guard or owned write. A
// discarded local copy of the flag does not establish meaningful resource use.
func gradedObservableProtocolRead(op *operation, u unit, index int) bool {
	body := op.body
	start := index
	for start > 0 && body[start-1].text != ";" && body[start-1].text != "{" && body[start-1].text != "}" {
		start--
	}
	for i := start; i < index; i++ {
		if body[i].text == "return" {
			return true
		}
		if body[i].text == "=" && i > 0 && gradeOwnedFieldReference(op, u, body, i-1) {
			if _, ok := callerDeclaredFields(u, op.owner)[body[i-1].text]; ok {
				return true
			}
		}
	}
	for i := 0; i < index; i++ {
		if body[i].text == "if" {
			conditionStart, conditionEnd := i+1, i+1
			if conditionStart < len(body) && body[conditionStart].text == "(" {
				conditionEnd = matching(body, conditionStart, "(", ")")
			} else {
				for conditionEnd < len(body) && body[conditionEnd].text != "{" {
					conditionEnd++
				}
			}
			if index > conditionStart && index < conditionEnd && hasValidationContext(body, op.language, gradedBuiltinPanic(op, u)) {
				return true
			}
		}
		if op.language == "rust" && (body[i].text == "assert" || body[i].text == "assert_eq") && i+2 < len(body) && body[i+1].text == "!" && body[i+2].text == "(" && matching(body, i+2, "(", ")") > index {
			return true
		}
	}
	return false
}

// The lexer keeps some compound operators as two tokens. Recognize their
// storage effect before deciding whether the final value is a literal reset.
func gradedStorageWriteAt(body []token, field int) bool {
	if field+1 >= len(body) {
		return false
	}
	switch body[field+1].text {
	case "=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", ">>>=", "&&=", "||=", "??=", "++", "--":
		return true
	case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>", ">>>", "&&", "||", "??":
		return field+2 < len(body) && body[field+2].text == "="
	}
	start := gradedStorageReferenceStart(body, field)
	return start > 0 && (body[start-1].text == "++" || body[start-1].text == "--")
}

// The protected region includes every catch attached to this try; finally
// executes for exits from both the try body and its catch handlers.
func gradedTryProtected(body []token, finally int) (int, int) {
	for i := finally - 1; i >= 0; i-- {
		if body[i].text != "try" || i+1 >= len(body) || body[i+1].text != "{" {
			continue
		}
		close := matching(body, i+1, "{", "}")
		if close < 0 {
			continue
		}
		next := close + 1
		for next < finally && body[next].text == "catch" {
			start := next + 1
			if start < len(body) && body[start].text == "(" {
				end := matching(body, start, "(", ")")
				if end < 0 {
					break
				}
				start = end + 1
			}
			if start >= len(body) || body[start].text != "{" {
				break
			}
			end := matching(body, start, "{", "}")
			if end < 0 {
				break
			}
			next = end + 1
		}
		if next == finally && gradedUnconditional(body, i) {
			return i + 2, finally - 1
		}
	}
	return -1, -1
}

func gradedCallObservesProtocol(op *operation, u unit, units []unit, c call, byKey map[string][]*operation, fields map[string]string) bool {
	candidates := gradedCleanupCandidates(op, u, units, c, byKey)
	if len(candidates) != 1 {
		return false
	}
	candidate := candidates[0]
	for i, tok := range candidate.body {
		if fields[tok.text] != "" && gradeOwnedFieldReference(candidate, units[candidate.file], candidate.body, i) && !gradedStorageWriteAt(candidate.body, i) && gradedProtocolReadAdmits(candidate, units[candidate.file], i, fields[tok.text]) {
			return true
		}
	}
	return false
}

func gradedOppositeBoolean(value string) string {
	if value == "true" {
		return "false"
	}
	return "true"
}
func gradedHasBooleanWrite(op *operation, units []unit, field, value string) bool {
	for _, found := range gradedBooleanWrites(op, units, value) {
		if found == field {
			return true
		}
	}
	return false
}

// Admission is a relationship between a state and a rejected/asserted condition,
// not a preferred Boolean encoding. Unsupported predicates remain unproven.
func gradedProtocolReadAdmits(op *operation, u unit, index int, reset string) bool {
	body := op.body
	for i := 0; i < index; i++ {
		assertion := op.language == "rust" && body[i].text == "assert" && i+2 < len(body) && body[i+1].text == "!"
		if body[i].text != "if" && !assertion {
			continue
		}
		start := i + 1
		if assertion {
			start = i + 2
		}
		if start >= len(body) {
			continue
		}
		end, after := start, start
		if body[start].text == "(" {
			end = matching(body, start, "(", ")")
			if end < 0 {
				continue
			}
			start++
			after = end + 1
		} else {
			for end < len(body) && body[end].text != "{" {
				end++
			}
			after = end
		}
		if index < start || index >= end {
			continue
		}
		value, known := gradedBooleanCondition(body[start:end], body[index].text, reset == "true")
		if !known {
			return false
		}
		if assertion {
			return !value
		}
		branch, next := gradedProtocolStatement(body, after)
		opposite := body[next:]
		if len(opposite) > 0 && opposite[0].text == "else" {
			opposite, _ = gradedProtocolStatement(body, next+1)
		}
		// The reset state must select the rejecting branch. The other branch
		// must expose an admitted return, rather than unrelated validation.
		rejects, admits := gradedProtocolOutcome(op, u, branch), gradedProtocolOutcome(op, u, opposite)
		if value {
			return rejects == -1 && admits == 1
		}
		return rejects == 1 && admits == -1
	}
	return gradedObservableProtocolRead(op, u, index)
}

// Outcomes are deliberately bounded to straight-line exits; unrelated nested
// predicates and calls are not proofs of this predicate's admission contract.
func gradedProtocolOutcome(op *operation, u unit, body []token) int {
	for len(body) > 0 && body[0].text == ";" {
		body = body[1:]
	}
	if len(body) == 0 {
		return 0
	}
	if body[0].text == "throw" && (op.language == "java" || op.language == "typescript") {
		return -1
	}
	if body[0].text == "panic" && op.language == "go" && gradedBuiltinPanic(op, u) && len(body) > 1 && body[1].text == "(" {
		return -1
	}
	if op.language == "rust" && body[0].text == "panic" && len(body) > 1 && body[1].text == "!" {
		return -1
	}
	if body[0].text == "return" {
		return 1
	}
	if op.language == "rust" && len(body) == 1 && (body[0].kind == "number" || body[0].text == "true" || body[0].text == "false") {
		return 1
	}
	// A void admitted path may perform observable work rather than return a
	// value (for example appending into owned accumulated state).
	if !gradedBodyHasControl(body) {
		fields := callerDeclaredFields(u, op.owner)
		for i := range body {
			_, declared := fields[body[i].text]
			if declared && gradeOwnedFieldReference(op, u, body, i) && gradedStorageWriteAt(body, i) {
				return 1
			}
		}
	}
	return 0
}

func gradedProtocolStatement(body []token, start int) ([]token, int) {
	if start >= len(body) {
		return nil, len(body)
	}
	if body[start].text == "{" {
		end := matching(body, start, "{", "}")
		if end < 0 {
			return nil, len(body)
		}
		return body[start+1 : end], end + 1
	}
	end := start
	for end < len(body) && body[end].text != ";" {
		end++
	}
	next := end
	if next < len(body) {
		next++
	}
	return body[start:end], next
}

func gradedOperationWritesField(op *operation, u unit, field string) bool {
	for i, tok := range op.body {
		if tok.text == field && gradeOwnedFieldReference(op, u, op.body, i) && gradedStorageWriteAt(op.body, i) {
			return true
		}
	}
	return false
}

func gradedBooleanCondition(body []token, field string, value bool) (bool, bool) {
	for len(body) > 1 && body[0].text == "(" && matching(body, 0, "(", ")") == len(body)-1 {
		body = body[1 : len(body)-1]
	}
	if len(body) > 0 && body[0].text == "!" {
		v, ok := gradedBooleanCondition(body[1:], field, value)
		return !v, ok
	}
	if len(body) == 1 && body[0].text == field {
		return value, true
	}
	if len(body) == 3 && body[1].text == "." && body[2].text == field {
		return value, true
	}
	for i, t := range body {
		if t.text == "==" || t.text == "!=" {
			a, ok := gradedBooleanCondition(body[:i], field, value)
			if !ok || i+2 != len(body) {
				return false, false
			}
			b := body[i+1].text
			if b != "true" && b != "false" {
				return false, false
			}
			result := a == (b == "true")
			if t.text == "!=" {
				result = !result
			}
			return result, true
		}
	}
	return false, false
}
