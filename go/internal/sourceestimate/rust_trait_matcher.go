package sourceestimate

// rustTraitMatcher is a byte-based Aho-Corasick index. Matching bytes preserves
// strings.Contains semantics, including partial names and UTF-8. Failure outputs
// are linked rather than copied, so suffix patterns do not inflate the index.
type rustTraitMatcher struct {
	nodes     []rustTraitMatchNode
	remaining []int
}

type rustTraitMatchNode struct {
	edges        map[byte]int
	next         map[byte]int
	fail, output int
	trait        string
}

func newRustTraitMatcher(names map[string]bool) *rustTraitMatcher {
	m := &rustTraitMatcher{nodes: []rustTraitMatchNode{{}}}
	for name := range names {
		state := 0
		for i := 0; i < len(name); i++ {
			child, ok := m.nodes[state].edges[name[i]]
			if !ok {
				child = len(m.nodes)
				m.nodes = append(m.nodes, rustTraitMatchNode{})
				if m.nodes[state].edges == nil {
					m.nodes[state].edges = map[byte]int{}
				}
				m.nodes[state].edges[name[i]] = child
			}
			state = child
		}
		m.nodes[state].trait = name
	}
	queue := make([]int, 0, len(m.nodes)-1)
	for _, child := range m.nodes[0].edges {
		queue = append(queue, child)
	}
	for head := 0; head < len(queue); head++ {
		state := queue[head]
		for b, child := range m.nodes[state].edges {
			fail := m.advance(m.nodes[state].fail, b)
			m.nodes[child].fail = fail
			m.nodes[child].output = m.nodes[fail].output
			if m.nodes[fail].trait != "" {
				m.nodes[child].output = fail
			}
			queue = append(queue, child)
		}
	}
	m.remaining = make([]int, len(m.nodes))
	for state := range m.nodes {
		m.remaining[state] = m.nodes[state].output
		if m.nodes[state].trait != "" {
			m.remaining[state] = state
		}
	}
	return m
}

// Cache missing transitions too. Each node/byte pair is resolved once across
// index construction and all signatures; the byte alphabet is fixed at 256.
func (m *rustTraitMatcher) advance(state int, b byte) int {
	if child, ok := m.nodes[state].edges[b]; ok {
		return child
	}
	if child, ok := m.nodes[state].next[b]; ok {
		return child
	}
	child := 0
	if state != 0 {
		child = m.advance(m.nodes[state].fail, b)
	}
	if m.nodes[state].next == nil {
		m.nodes[state].next = map[byte]int{}
	}
	m.nodes[state].next[b] = child
	return child
}

// Every trait is emitted once over the matcher's package-analysis lifetime.
// Compressed skip links retire matched suffixes, so repeated signatures do not
// repeatedly enumerate overlapping names that are already known to escape.
func (m *rustTraitMatcher) unmatched(state int) int {
	if m.remaining[state] != state {
		m.remaining[state] = m.unmatched(m.remaining[state])
	}
	return m.remaining[state]
}

func (m *rustTraitMatcher) match(signature string, emit func(string)) {
	state := 0
	for i := 0; i < len(signature); i++ {
		state = m.advance(state, signature[i])
		for output := m.unmatched(state); output != 0; output = m.unmatched(output) {
			emit(m.nodes[output].trait)
			m.remaining[output] = m.unmatched(m.nodes[output].output)
		}
	}
}
