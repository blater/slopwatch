package depth

const maxOutcomeElementVisits = 1000000

type outcomeWalker struct {
	analyzer *recipeAnalyzer
	out      Outcome
	seen     map[RecipeID]bool
	stopped  bool
}

func (w *outcomeWalker) fail(reason string) { w.out.Unknown = true; w.out.Reason = reason }
func (w *outcomeWalker) charge() bool {
	if w.stopped {
		return false
	}
	if w.out.ElementVisits >= maxOutcomeElementVisits {
		w.fail("work_limit")
		w.stopped = true
		return false
	}
	w.out.ElementVisits++
	return true
}
func (w *outcomeWalker) edge(child RecipeID) {
	if w.charge() {
		w.visit(child)
	}
}
func (w *outcomeWalker) visit(id RecipeID) {
	if w.seen[id] || w.stopped {
		return
	}
	if !w.charge() {
		return
	}
	node, ok := w.analyzer.nodes[id]
	if !ok {
		w.fail(ReasonUnknownRecipe)
		return
	}
	w.seen[id] = true
	if len(w.seen) > w.analyzer.maxNodes {
		w.fail(ReasonRecipeLimit)
		w.stopped = true
		return
	}
	if node.Kind == KindUnknown {
		w.fail(ReasonUnknownOperand)
	}
	for _, child := range node.Children {
		if w.stopped {
			return
		}
		w.edge(child)
	}
	for _, field := range node.Fields {
		if w.stopped {
			return
		}
		w.edge(field.Recipe)
	}
	if node.Predicate != "" {
		w.edge(node.Predicate)
		w.edge(node.TrueValue)
		w.edge(node.FalseValue)
	}
}
