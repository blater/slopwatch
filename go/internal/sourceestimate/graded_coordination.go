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

// A reset of an owned boolean protocol field is a bounded, spelling-independent
// cleanup witness. Arbitrary mutations (e.g. logging counters) are not cleanup.

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

// Reject effects nested inside a conditional/loop, including unbraced bodies.
// Admission guards that exit before a later unconditional write remain valid.

// A protocol read must affect an outcome, admission guard or owned write. A
// discarded local copy of the flag does not establish meaningful resource use.

// The lexer keeps some compound operators as two tokens. Recognize their
// storage effect before deciding whether the final value is a literal reset.

// The protected region includes every catch attached to this try; finally
// executes for exits from both the try body and its catch handlers.

// Admission is a relationship between a state and a rejected/asserted condition,
// not a preferred Boolean encoding. Unsupported predicates remain unproven.

// Outcomes are deliberately bounded to straight-line exits; unrelated nested
// predicates and calls are not proofs of this predicate's admission contract.
