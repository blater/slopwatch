package depth

// Outcome is the bounded accounting result for one returned recipe.
type Outcome struct {
	Recipe        RecipeID
	UniqueNodes   int
	ElementVisits int
	Unknown       bool
	Reason        string
}

// BuildOutcome stops at the node/visit bounds during traversal, rather than
// first expanding an arbitrarily large graph and checking its size afterwards.
// Each operand position is charged once, even when two positions share a node.
func (a *recipeAnalyzer) BuildOutcome(id RecipeID) Outcome {
	walker := outcomeWalker{analyzer: a, out: Outcome{Recipe: id}, seen: map[RecipeID]bool{}}
	walker.visit(id)
	walker.out.UniqueNodes = len(walker.seen)
	return walker.out
}

func (a *recipeMetadata) NodeCount() int { return len(a.nodes) }
func (a *recipeMetadata) MaxNodes() int  { return a.maxNodes }
