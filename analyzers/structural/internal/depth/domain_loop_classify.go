package depth

// Numeric loop classification concerns the connected value relation only. CFG
// evaluation must separately establish supported exits and absence of unresolved
// loop effects. No iteration count, termination or algorithmic correctness claim
// follows from this classification.
type loopValueAnalysis struct {
	analyzer       *recipeAnalyzer
	slots          map[string]recipeNode
	seen           map[RecipeID]bool
	visitingSlots  map[string]bool
	input, unknown bool
}

func classifyNumericLoop(a *recipeAnalyzer, n recipeNode) TransformationClass {
	l := loopValueAnalysis{analyzer: a, slots: map[string]recipeNode{}, seen: map[RecipeID]bool{}, visitingSlots: map[string]bool{}}
	for _, field := range n.Fields {
		binding, ok := a.nodes[field.Recipe]
		if !ok || binding.Kind != KindLoopBinding || len(binding.Children) != 2 {
			return TransformationUnknown
		}
		l.slots[field.Field] = binding
	}
	result, ok := l.slots[n.RecurrenceID]
	if !ok || len(n.Children) != 2 || result.Type != n.Type {
		return TransformationUnknown
	}
	initial, update := result.Children[0], result.Children[1]
	condition := a.nodes[n.Children[1]]
	if condition.Kind == KindConstant && condition.Literal == "false" {
		return a.Classify(initial)
	}
	step := a.nodes[update]
	if update == initial || (step.Kind == KindRecurrence && step.RecurrenceID == n.RecurrenceID && len(step.Children) == 0) {
		return a.Classify(initial)
	}
	// Cross-slot copies require relational fixed-point normalization; do not
	// invent a transformation merely because two slot identities differ.
	if step.Kind == KindRecurrence {
		return TransformationUnknown
	}
	l.slot(n.RecurrenceID)
	l.visit(n.Children[0])
	if l.unknown {
		return TransformationUnknown
	}
	if !l.input {
		return TransformationConstant
	}
	return TransformationPrimitive
}
func (l *loopValueAnalysis) slot(id string) {
	if l.visitingSlots[id] {
		return
	}
	binding, ok := l.slots[id]
	if !ok {
		l.unknown = true
		return
	}
	l.visitingSlots[id] = true
	if l.analyzer.Classify(binding.Children[0]) == TransformationUnknown {
		l.unknown = true
		return
	}
	l.visit(binding.Children[0])
	l.visit(binding.Children[1])
}
func (l *loopValueAnalysis) visit(id RecipeID) {
	if l.seen[id] {
		return
	}
	l.seen[id] = true
	n, ok := l.analyzer.nodes[id]
	if !ok {
		l.unknown = true
		return
	}
	switch n.Kind {
	case KindFormal, KindStorage:
		l.input = true
	case KindConstant:
		return
	case KindRecurrence:
		if len(n.Children) != 0 {
			l.unknown = true
			return
		}
		binding, ok := l.slots[n.RecurrenceID]
		if !ok || binding.Type != n.Type {
			l.unknown = true
			return
		}
		l.slot(n.RecurrenceID)
		return
	case KindPrimitive:
		if !supportedOperator(n.Operator, n.ArithmeticMode) {
			l.unknown = true
			return
		}
	case KindSelect:
		l.visit(n.Predicate)
		l.visit(n.TrueValue)
		l.visit(n.FalseValue)
		return
	case KindFieldRead:
	default:
		l.unknown = true
		return
	}
	for _, child := range n.Children {
		l.visit(child)
	}
}
