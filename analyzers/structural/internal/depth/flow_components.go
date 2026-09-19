package depth

import "sort"

// graphComponents is iterative Tarjan traversal. It is shared by loop and call
// graph scheduling; component membership does not itself establish behavior.
type graphComponents struct {
	adjacency  map[string][]string
	charge     func(int) bool
	next       int
	index, low map[string]int
	onStack    map[string]bool
	stack      []string
	result     [][]string
}
type componentFrame struct {
	id   string
	next int
}

func stronglyConnected(adjacency map[string][]string, charge func(int) bool) ([][]string, bool) {
	g := &graphComponents{adjacency: adjacency, charge: charge, index: map[string]int{}, low: map[string]int{}, onStack: map[string]bool{}}
	ids := make([]string, 0, len(adjacency))
	for id := range adjacency {
		if !charge(1) {
			return nil, false
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if g.index[id] == 0 && !g.visit(id) {
			return nil, false
		}
	}
	sort.Slice(g.result, func(i, j int) bool { return g.result[i][0] < g.result[j][0] })
	return g.result, true
}
func (g *graphComponents) enter(id string) {
	g.next++
	g.index[id] = g.next
	g.low[id] = g.next
	g.onStack[id] = true
	g.stack = append(g.stack, id)
}
func (g *graphComponents) visit(start string) bool {
	g.enter(start)
	frames := []componentFrame{{id: start}}
	for len(frames) > 0 {
		if !g.charge(1) {
			return false
		}
		frame := &frames[len(frames)-1]
		edges := g.adjacency[frame.id]
		if frame.next < len(edges) {
			child := edges[frame.next]
			frame.next++
			if g.index[child] == 0 {
				g.enter(child)
				frames = append(frames, componentFrame{id: child})
				continue
			}
			if g.onStack[child] && g.index[child] < g.low[frame.id] {
				g.low[frame.id] = g.index[child]
			}
			continue
		}
		id := frame.id
		frames = frames[:len(frames)-1]
		if len(frames) > 0 {
			parent := frames[len(frames)-1].id
			if g.low[id] < g.low[parent] {
				g.low[parent] = g.low[id]
			}
		}
		if g.low[id] == g.index[id] && !g.extract(id) {
			return false
		}
	}
	return true
}
func (g *graphComponents) extract(root string) bool {
	var component []string
	for len(g.stack) > 0 {
		if !g.charge(1) {
			return false
		}
		index := len(g.stack) - 1
		id := g.stack[index]
		g.stack = g.stack[:index]
		g.onStack[id] = false
		component = append(component, id)
		if id == root {
			break
		}
	}
	sort.Strings(component)
	g.result = append(g.result, component)
	return true
}
