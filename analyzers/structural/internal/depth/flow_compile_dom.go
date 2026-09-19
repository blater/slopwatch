package depth

import "sort"

func dominates(c *CompiledFunction, def, use string) bool {
	if c.cancelled || c.workExceeded {
		return false
	}
	if def == use {
		return true
	}
	if c.idom == nil {
		computeDominators(c)
	}
	preDef, okDef := c.domPre[def]
	preUse, okUse := c.domPre[use]
	postDef, postDefOK := c.domPost[def]
	postUse, postUseOK := c.domPost[use]
	return okDef && okUse && postDefOK && postUseOK && preDef <= preUse && postUse <= postDef
}

func computeDominators(c *CompiledFunction) {
	postorder := []string{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] || c.cancelled || c.workExceeded {
			return
		}
		if !chargeCompilerWork(c, 1) {
			return
		}
		seen[id] = true
		edges := effectiveEdges(c.blocks[id].Block)
		sort.Slice(edges, func(i, j int) bool { return edges[i].To < edges[j].To })
		for _, edge := range edges {
			if !chargeCompilerWork(c, 1) {
				return
			}
			if c.reachableSet[edge.To] {
				visit(edge.To)
			}
		}
		postorder = append(postorder, id)
	}
	visit(c.function.Entry)
	if c.cancelled || c.workExceeded || len(postorder) == 0 {
		return
	}
	order := make([]string, len(postorder))
	for i := range postorder {
		if !chargeCompilerWork(c, 1) {
			return
		}
		order[len(postorder)-1-i] = postorder[i]
	}
	c.rpoIndex = map[string]int{}
	for i, id := range order {
		if !chargeCompilerWork(c, 1) {
			return
		}
		c.rpoIndex[id] = i
	}
	c.idom = map[string]string{c.function.Entry: c.function.Entry}
	changed := true
	for changed && !c.cancelled && !c.workExceeded {
		changed = false
		for _, id := range order[1:] {
			if cancelled(c.ctx) {
				c.cancelled = true
				break
			}
			var next string
			for _, pred := range c.predecessors[id] {
				if c.idom[pred] == "" {
					continue
				}
				if next == "" {
					next = pred
				} else {
					next = intersectDom(c, next, pred)
				}
				if !chargeCompilerWork(c, 1) {
					break
				}
			}
			if c.workExceeded {
				break
			}
			if next != "" && c.idom[id] != next {
				c.idom[id] = next
				changed = true
			}
		}
	}
	c.domPre, c.domPost = map[string]int{}, map[string]int{}
	children := map[string][]string{}
	for child, parent := range c.idom {
		if !chargeCompilerWork(c, 1) {
			return
		}
		if child != parent {
			children[parent] = append(children[parent], child)
		}
	}
	for parent := range children {
		sort.Strings(children[parent])
	}
	tick := 0
	var walk func(string)
	walk = func(id string) {
		if !chargeCompilerWork(c, 1) {
			return
		}
		c.domPre[id] = tick
		tick++
		for _, child := range children[id] {
			walk(child)
		}
		c.domPost[id] = tick
		tick++
	}
	if !c.cancelled && !c.workExceeded {
		walk(c.function.Entry)
	}
}

func intersectDom(c *CompiledFunction, left, right string) string {
	for left != right {
		for c.rpoIndex[left] > c.rpoIndex[right] {
			if !chargeCompilerWork(c, 1) {
				return left
			}
			left = c.idom[left]
		}
		for c.rpoIndex[right] > c.rpoIndex[left] {
			if !chargeCompilerWork(c, 1) {
				return right
			}
			right = c.idom[right]
		}
	}
	return left
}
