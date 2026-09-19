package depth

// guardID indexes a reduced ordered decision DAG. False and true are terminals.
// A reconverged diamond reduces to its incoming condition, without enumerating
// paths. Every recursive operation is memoized and charged to the evaluator.
type guardID int

const (
	guardFalse guardID = 0
	guardTrue  guardID = 1
)

type guardNode struct {
	predicate RecipeID
	yes, no   guardID
}
type guardPair struct{ a, b guardID }
type controlGuards struct {
	atoms   map[RecipeID]guardID
	nodes   []guardNode
	intern  map[guardNode]guardID
	andMemo map[guardPair]guardID
	notMemo map[guardID]guardID
	recipes map[guardID]RecipeID
	arena   *RecipeArena
	charge  func(int) bool
}

func newControlGuards(arena *RecipeArena, charge func(int) bool) *controlGuards {
	return &controlGuards{atoms: map[RecipeID]guardID{}, nodes: make([]guardNode, 2), intern: map[guardNode]guardID{}, andMemo: map[guardPair]guardID{}, notMemo: map[guardID]guardID{}, recipes: map[guardID]RecipeID{guardFalse: arena.Constant("bool", "false"), guardTrue: arena.Constant("bool", "true")}, arena: arena, charge: charge}
}
func (g *controlGuards) node(n guardNode) guardID {
	if n.yes == n.no {
		return n.yes
	}
	if id, ok := g.intern[n]; ok {
		return id
	}
	id := guardID(len(g.nodes))
	g.nodes = append(g.nodes, n)
	g.intern[n] = id
	return id
}

func (g *controlGuards) not(id guardID) guardID {
	if !g.charge(1) {
		return guardFalse
	}
	if id == guardTrue {
		return guardFalse
	}
	if id == guardFalse {
		return guardTrue
	}
	if out, ok := g.notMemo[id]; ok {
		return out
	}
	n := g.nodes[id]
	out := g.node(guardNode{n.predicate, g.not(n.yes), g.not(n.no)})
	g.notMemo[id] = out
	return out
}
func (g *controlGuards) and(a, b guardID) guardID {
	if !g.charge(1) {
		return guardFalse
	}
	if a == guardFalse || b == guardFalse {
		return guardFalse
	}
	if a == guardTrue {
		return b
	}
	if b == guardTrue || a == b {
		return a
	}
	if a > b {
		a, b = b, a
	}
	key := guardPair{a, b}
	if out, ok := g.andMemo[key]; ok {
		return out
	}
	x, y := g.nodes[a], g.nodes[b]
	p := x.predicate
	if y.predicate < p {
		p = y.predicate
	}
	ay, an := g.cofactors(a, p)
	by, bn := g.cofactors(b, p)
	out := g.node(guardNode{p, g.and(ay, by), g.and(an, bn)})
	g.andMemo[key] = out
	return out
}
func (g *controlGuards) cofactors(id guardID, p RecipeID) (guardID, guardID) {
	n := g.nodes[id]
	if n.predicate == p {
		return n.yes, n.no
	}
	return id, id
}
func (g *controlGuards) or(a, b guardID) guardID { return g.not(g.and(g.not(a), g.not(b))) }
func (g *controlGuards) recipe(id guardID) RecipeID {
	if !g.charge(1) {
		return UnknownRecipeID
	}
	if r, ok := g.recipes[id]; ok {
		return r
	}
	n := g.nodes[id]
	if n.yes == guardTrue && n.no == guardFalse {
		g.recipes[id] = n.predicate
		return n.predicate
	}
	r, _ := g.arena.Select("bool", n.predicate, g.recipe(n.yes), g.recipe(n.no))
	g.recipes[id] = r
	return r
}
