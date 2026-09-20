package sourceestimate

// Parenthesis pairs and fluent roots depend only on the immutable body. The
// pair stack deliberately ignores other delimiter kinds, matching matching().
type gradedCallerBodyIndex struct{ pairs, roots []int }

func indexGradedCallerBody(body []token) gradedCallerBodyIndex {
	index := newGradedCallerBodyIndex(len(body))
	stack := []int{}
	for i, tok := range body {
		index.recordParenthesis(tok.text, i, &stack)
		index.roots[i] = gradedCallerRootAt(body, index, i)
	}
	return index
}

func newGradedCallerBodyIndex(size int) gradedCallerBodyIndex {
	index := gradedCallerBodyIndex{pairs: make([]int, size), roots: make([]int, size)}
	for i := range index.pairs {
		index.pairs[i] = -1
	}
	return index
}

func (index gradedCallerBodyIndex) recordParenthesis(text string, position int, stack *[]int) {
	if text == "(" {
		*stack = append(*stack, position)
		return
	}
	if text != ")" || len(*stack) == 0 {
		return
	}
	open := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	index.pairs[open] = position
	index.pairs[position] = open
}

func gradedCallerRootAt(body []token, index gradedCallerBodyIndex, position int) int {
	root := position
	if position < 2 || body[position-1].text != "." {
		return root
	}
	if body[position-2].text != ")" {
		return index.roots[position-2]
	}
	open := index.pairs[position-2]
	if open < 1 {
		return -1
	}
	return index.roots[open-1]
}

func (index gradedCallerBodyIndex) callRoot(body []token, c call) string {
	if c.position < 0 || c.position >= len(body) {
		return ""
	}
	root := index.roots[c.position]
	if root < 0 {
		return ""
	}
	return body[root].text
}

func gradedCallerCallRoot(body []token, c call) string {
	return indexGradedCallerBody(body).callRoot(body, c)
}

func gradedCallerPredicateRefs(body []token) map[int]int {
	return gradedCallerPredicateRefsIndexed(body, indexGradedCallerBody(body))
}

func gradedCallerPredicateRefsIndexed(body []token, index gradedCallerBodyIndex) map[int]int {
	refs := map[int]int{}
	ends := []int{}
	for i := range body {
		for len(ends) > 0 && ends[len(ends)-1] <= i {
			ends = ends[:len(ends)-1]
		}
		if i >= 2 && body[i-2].text == "if" && body[i-1].text == "(" && index.pairs[i-1] > i {
			ends = append(ends, index.pairs[i-1])
		}
		if len(ends) > 0 {
			refs[i] = ends[len(ends)-1] + 1
		}
	}
	return refs
}
