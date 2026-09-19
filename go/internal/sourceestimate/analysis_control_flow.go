package sourceestimate

// unconditionalQueries indexes one immutable exact body slice in one analysis.
// Answers are built lazily in linear time and retain one byte per position.
// Contextual copies may share them: owner and bindings do not affect control flow.
// Like operationBodies, initialization occurs in sequential analysis passes only.
type unconditionalQueries struct {
	answers []uint8
	source  *token
	length  int
	isNil   bool
}

// Valid positions are 0 through len(normalizedEagerBody(op)), inclusive.
// Nonpositive positions are unconditionally true in the original implementation.
// Positions past the body retain its fallback behavior, including possible panic.
func normalizedEagerUnconditional(op *operation, position int) bool {
	if position <= 0 {
		return true
	}
	body := normalizedEagerBody(op) // Validates identity, length, language and nilness.
	return operationBodyUnconditional(op, body, position)
}

// The operation owns at most one exact-body flow index. Contextual copies may
// share a completed index, but replacing or slicing a body replaces the pointer
// rather than changing another copy's cache. No source tokens are mutated.
func operationBodyUnconditional(op *operation, body []token, position int) bool {
	if position <= 0 {
		return true
	}
	if op.normalized.flow == nil || !op.normalized.flow.matches(body) {
		op.normalized.flow = &unconditionalQueries{}
	}
	return op.normalized.flow.unconditional(body, position)
}

func (q *unconditionalQueries) matches(body []token) bool {
	var source *token
	if len(body) > 0 {
		source = &body[0]
	}
	return q.source == source && q.length == len(body) && q.isNil == (body == nil)
}

func (q *unconditionalQueries) unconditional(body []token, position int) bool {
	if position <= 0 {
		return true
	}
	if position > len(body) {
		return gradedUnconditional(body, position)
	}
	if q.answers == nil || !q.matches(body) {
		q.source = nil
		if len(body) > 0 {
			q.source = &body[0]
		}
		q.length, q.isNil = len(body), body == nil
		q.answers = buildUnconditionalAnswers(body)
	}
	return q.answers[position] == 2
}

func buildUnconditionalAnswers(body []token) []uint8 {
	n := len(body)
	// matching counts each delimiter kind independently, even for crossed or
	// malformed delimiters. Separate stack passes preserve that exact behavior.
	// Most methods are short. Keep transient indexes on the stack for those
	// bodies; large bodies still allocate only linear scratch space.
	var smallMatches, smallStack [128]int
	var smallDelta [130]int
	matches := smallMatches[:min(n, len(smallMatches))]
	stack := smallStack[:0]
	delta := smallDelta[:min(n+2, len(smallDelta))]
	if n > len(smallMatches) {
		matches = make([]int, n)
		stack = make([]int, 0, n)
		delta = make([]int, n+2)
	}
	for i := range matches {
		matches[i] = -1
	}
	for _, pair := range [][2]string{{"(", ")"}, {"{", "}"}} {
		stack = stack[:0]
		for i, tok := range body {
			if tok.text == pair[0] {
				stack = append(stack, i)
			}
			if tok.text == pair[1] && len(stack) > 0 {
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				matches[open] = i
			}
		}
	}
	// statementEnd ignores nesting and stops at the first semicolon or close
	// brace. Reuse stack storage once delimiter matching is complete.
	ends := stack[:n]
	nextEnd := n
	for i := n - 1; i >= 0; i-- {
		if body[i].text == ";" || body[i].text == "}" {
			nextEnd = i
		}
		ends[i] = nextEnd
	}
	nextBrace := n
	for i := n - 1; i >= 0; i-- {
		if gradedGuardKeyword(body[i].text) {
			start := i + 1
			invalid := false
			if start < n && body[start].text == "(" {
				invalid = matches[start] < 0
				start = matches[start] + 1
			} else if body[i].text != "else" {
				start = nextBrace
			}
			if invalid {
				// The reference rejects every later position for an unmatched
				// parenthesis, but ignores a guard with no remaining body.
				delta[i+1]++
				delta[n+1]--
			} else if start < n {
				end := ends[start]
				if body[start].text == "{" {
					end = matches[start]
				}
				if start <= end {
					delta[start]++
					delta[end+1]--
				}
			}
		}
		if body[i].text == "{" {
			nextBrace = i
		}
	}
	answers := make([]uint8, n+1)
	active, depth, exitEnd := 0, 0, n
	for i := 0; i <= n; i++ {
		active += delta[i]
		answers[i] = 1
		if active == 0 && i <= exitEnd {
			answers[i] = 2
		}
		if i == n {
			break
		}
		switch body[i].text {
		case "{":
			depth++
		case "}":
			depth--
		}
		isExit := body[i].text == "return" || body[i].text == "throw" || body[i].text == "panic" && i+1 < n && body[i+1].text == "("
		if depth == 0 && isExit && answers[i] == 2 && ends[i] < exitEnd {
			exitEnd = ends[i]
		}
	}
	return answers
}
