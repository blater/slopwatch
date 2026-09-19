package sourceestimate

import (
	"math"
	"strings"
)

// A transparent relay preserves inputs, output and the callable operation. It
// does not take ownership of a separately exposed protocol's implementation.
func gradeTransparentCall(op *operation) (call, bool) {
	if gradedSurfaceConstructor(op) {
		return call{}, false
	}
	body := pruneDeadFalseBranches(op.body)
	calls := callsIn(body)
	if len(calls) != 1 {
		return call{}, false
	}
	c := calls[0]
	if len(c.actuals) != len(op.paramNames) {
		return call{}, false
	}
	for i, arg := range c.actuals {
		for len(arg) > 2 && arg[0].text == "(" && matching(arg, 0, "(", ")") == len(arg)-1 {
			arg = arg[1 : len(arg)-1]
		}
		if len(arg) != 1 || arg[0].text != op.paramNames[i] {
			return call{}, false
		}
	}
	localResult := gradedCallAssignedName(body, c)
	if localResult != "" && !gradedCallResultOnly(op, body, c, localResult) {
		return call{}, false
	}
	for _, tok := range body {
		switch tok.text {
		case "=", ":=":
			if localResult == "" {
				return call{}, false
			}
		case "if", "for", "while", "match", "switch", "try", "defer", "finally", "+", "-", "*", "/", "?", "&&", "||", "throw":
			return call{}, false
		}
	}
	return c, true
}

func gradedCallResultOnly(op *operation, body []token, c call, local string) bool {
	assignments := 0
	for _, tok := range body {
		if tok.text == "=" || tok.text == ":=" {
			assignments++
		}
	}
	if assignments != 1 {
		return false
	}
	end := matching(body, c.position+1, "(", ")")
	if end < 0 {
		return false
	}
	rest := body[end+1:]
	for len(rest) > 0 && rest[0].text == ";" {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0].text == "return" {
		rest = rest[1:]
	} else if op.language != "rust" {
		return false
	}
	for len(rest) > 0 && rest[len(rest)-1].text == ";" {
		rest = rest[:len(rest)-1]
	}
	return len(rest) == 1 && rest[0].text == local
}

// gradeUnresolvedResultCall is deliberately separate from protocol transparency:
// validation may precede a returned conversion without discarding its unknown
// result duty. Recognize only a final, top-level call (or its immediately
// returned local). This bounded shape excludes nested closures, discarded calls
// and any intervening alias overwrite without a new control-flow analysis.
func gradeUnresolvedResultCall(op *operation) (call, bool) {
	if gradedSurfaceConstructor(op) {
		return call{}, false
	}
	body := pruneDeadFalseBranches(op.body)
	calls := callsIn(body)
	if len(calls) == 0 {
		return call{}, false
	}
	c := calls[len(calls)-1]
	if len(c.actuals) == 0 || len(c.actuals) != len(op.paramNames) {
		return call{}, false
	}
	for i, arg := range c.actuals {
		for len(arg) > 2 && arg[0].text == "(" && matching(arg, 0, "(", ")") == len(arg)-1 {
			arg = arg[1 : len(arg)-1]
		}
		if len(arg) != 1 || arg[0].text != op.paramNames[i] {
			return call{}, false
		}
	}
	start, end := c.position, matching(body, c.position+1, "(", ")")
	if end < 0 {
		return call{}, false
	}
	for start >= 2 && (body[start-1].text == "." || body[start-1].text == "::") && isReceiverPart(body[start-2]) {
		start -= 2
	}
	// Peel only value-preserving result wrappers, never another call or operator.
	for {
		switch {
		case start > 0 && body[start-1].text == "(" && matching(body, start-1, "(", ")") == end+1:
			start--
			end++
		case op.language == "typescript" && start > 0 && body[start-1].text == "await":
			start--
		case op.language == "typescript" && end+1 < len(body) && body[end+1].text == "as" && gradedResultTypeEnd(body, end+2) > end+2:
			end = gradedResultTypeEnd(body, end+2) - 1
		case op.language == "java" && start > 2 && body[start-1].text == ")":
			cast := start - 2
			for cast > 0 && body[cast].text != "(" {
				cast--
			}
			if body[cast].text != "(" || gradedResultTypeEnd(body, cast+1) != start-1 {
				return call{}, false
			}
			start = cast
		default:
			goto normalized
		}
	}
normalized:
	if !gradedUnconditionalResultPrefix(body[:start]) {
		return call{}, false
	}
	rest := body[end+1:]
	if start > 0 && body[start-1].text == "return" {
		for len(rest) > 0 && rest[0].text == ";" {
			rest = rest[1:]
		}
		return c, len(rest) == 0
	}
	if start > 0 && (body[start-1].text == "=" || body[start-1].text == ":=") {
		local := gradedResultLocal(op.language, body, start-1)
		for len(rest) > 0 && rest[0].text == ";" {
			rest = rest[1:]
		}
		explicit := len(rest) > 0 && rest[0].text == "return"
		if explicit {
			rest = rest[1:]
			for len(rest) > 0 && rest[len(rest)-1].text == ";" {
				rest = rest[:len(rest)-1]
			}
		}
		for len(rest) > 2 && rest[0].text == "(" && matching(rest, 0, "(", ")") == len(rest)-1 {
			rest = rest[1 : len(rest)-1]
		}
		return c, local != "" && len(rest) == 1 && rest[0].text == local && (explicit || op.language == "rust")
	}
	// A Rust tail or expression-bodied arrow returns the expression itself;
	// a semicolon makes a Rust call a discarded statement.
	boundary := start == 0 || body[start-1].text == ";" || body[start-1].text == "}"
	return c, boundary && len(rest) == 0 && (op.expressionBody || op.language == "rust" && op.returnType != "" && op.returnType != "()")
}

// Qualified names and array suffixes cover the simple cast/type-assertion
// wrappers accepted here. Complex type expressions remain conservative.
func gradedResultTypeEnd(body []token, start int) int {
	if start >= len(body) || !isIdentifier(body[start].text) || isControl(body[start].text) {
		return start
	}
	end := start + 1
	for end+1 < len(body) && body[end].text == "." && isIdentifier(body[end+1].text) {
		end += 2
	}
	for end+1 < len(body) && body[end].text == "[" && body[end+1].text == "]" {
		end += 2
	}
	return end
}

// Delimiter depth alone cannot distinguish `if (x) return f(x)` from an
// unconditional return. Retain pending unbraced control until its statement
// terminates. This also rejects conditional assignments/declarations.
func gradedUnconditionalResultPrefix(body []token) bool {
	depth, conditional := 0, false
	for _, tok := range body {
		switch tok.text {
		case "if", "else", "for", "while", "switch", "match", "do":
			if depth == 0 {
				conditional = true
			}
		case "{", "(", "[":
			depth++
		case "}", ")", "]":
			depth--
			if tok.text == "}" && depth == 0 {
				conditional = false
			}
		case ";":
			if depth == 0 {
				conditional = false
			}
		}
	}
	return depth == 0 && !conditional
}

// The assignment must introduce a local binding. Reading its adjacent syntax
// avoids confusing an earlier semicolonless declaration with this call, or an
// unqualified Java field assignment with a returned local.
func gradedResultLocal(language string, body []token, assign int) string {
	name := assign - 1
	if name < 0 {
		return ""
	}
	if language == "typescript" || language == "rust" {
		for i := name - 1; i >= 1 && body[i].text != "=" && body[i].text != ";"; i-- {
			if body[i].text == ":" && gradedResultTypeEnd(body, i+1) == assign {
				name = i - 1
				break
			}
		}
	}
	if !isIdentifier(body[name].text) {
		return ""
	}
	declaration := name - 1
	switch language {
	case "typescript":
		if declaration < 0 || (body[declaration].text != "const" && body[declaration].text != "let" && body[declaration].text != "var") {
			return ""
		}
	case "rust":
		if declaration >= 0 && body[declaration].text == "mut" {
			declaration--
		}
		if declaration < 0 || body[declaration].text != "let" {
			return ""
		}
	case "go":
		if body[assign].text != ":=" || declaration >= 0 && (body[declaration].text == "." || body[declaration].text == ",") {
			return ""
		}
	case "java":
		for declaration >= 0 && body[declaration].text != ";" && body[declaration].text != "}" && body[declaration].text != "{" {
			declaration--
		}
		declaration++
		if declaration < name && body[declaration].text == "final" {
			declaration++
		}
		if declaration >= name || gradedResultTypeEnd(body, declaration) != name {
			return ""
		}
	default:
		return ""
	}
	return body[name].text
}

// An unavailable input-to-result implementation leaves two specific duties
// possible at the call site: checking the accepted input domain and producing
// the result. Use that bounded duty envelope for the estimated projection,
// separately from observed responsibility. It is one connected outcome, not
// one credit per unknown call. Already observed duties are not counted again.
// Calls on owned delegates retain their observable protocol attribution;
// unrelated calls and discarded results do not acquire this allowance.
func gradeUnresolvedReturnedDuty(roots []*operation, units []unit, byKey map[string][]*operation, known map[string]float64, p CalibrationProfile) float64 {
	allowance := 0.0
	for _, root := range roots {
		for _, op := range gradeOwnedOperations(root, units, byKey) {
			if connected, predicateOnly, representationOnly := gradeConnectedResult(op, units, byKey); connected {
				if representationOnly {
					transformation := known["transform"] + known["representation-transformation"] + known["invariant-transformation"]
					allowance = math.Max(allowance, math.Max(0, p.TransformationEnvelope-transformation))
					continue
				}
				if !predicateOnly {
					return gradeResultDutyEnvelopeWithCalibration(known, p)
				}
				allowance = math.Max(allowance, math.Max(0, p.ValidationEnvelope-known["validation"]))
				continue
			}
			// Composing several stages of an owned delegate also takes over
			// admission of inputs to that protocol. Missing its implementation
			// must not erase that possible validation duty.
			if known["coordination"] > 0 && known["validation"] < p.ValidationEnvelope {
				for _, candidate := range callsIn(pruneDeadFalseBranches(op.body)) {
					dot := strings.LastIndexByte(candidate.name, '.')
					if dot >= 0 && len(candidate.actuals) > 0 && gradeOwnedReceiver(op, units[op.file], candidate.name[:dot]) && len(resolveCall(op, candidate, units, byKey)) == 0 {
						allowance = p.ValidationEnvelope - known["validation"]
					}
				}
			}
			c, ok := gradeUnresolvedResultCall(op)
			if !ok || len(c.actuals) == 0 || len(resolveCall(op, c, units, byKey)) != 0 {
				continue
			}
			if dot := strings.LastIndexByte(c.name, '.'); dot >= 0 && gradeOwnedReceiver(op, units[op.file], c.name[:dot]) {
				continue
			}
			return gradeResultDutyEnvelopeWithCalibration(known, p)
		}
	}
	return allowance
}

func gradeResultDutyEnvelope(known map[string]float64) float64 {
	return gradeResultDutyEnvelopeWithCalibration(known, DefaultCalibration())
}

func gradeResultDutyEnvelopeWithCalibration(known map[string]float64, p CalibrationProfile) float64 {
	transformation := known["transform"] + known["representation-transformation"] + known["invariant-transformation"]
	return math.Max(0, p.ValidationEnvelope-known["validation"]) + math.Max(0, p.TransformationEnvelope-transformation)
}
