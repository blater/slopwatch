package depth

import "slopslap.dev/structural/internal/facts"

type numericRegion struct {
	header     string
	order      []string
	slots      []CompiledRecurrence
	body, exit facts.FlowEdge
}

// Admit single-header numeric loops with an acyclic iteration and a header exit.
// Nested/irreducible loops and early exits remain explicit gaps until their
// completion semantics are implemented; they never masquerade as empty bodies.
func numericRegionShape(e *controlEvaluation, members []string) (numericRegion, bool) {
	p := numericRegion{}
	inside := map[string]bool{}
	for _, id := range members {
		inside[id] = true
	}
	if !numericRegionSlots(e, &p, inside) {
		return p, false
	}
	graph := map[string][]string{}
	for _, id := range members {
		if !numericRegionBlock(e, &p, id, inside, graph) {
			return p, false
		}
	}
	var ok bool
	p.order, ok = orderControlGraph(graph, e.budget.charge)
	if !ok || !numericHeaderShape(e, &p, inside) {
		return p, false
	}
	return p, true
}
func numericRegionSlots(e *controlEvaluation, p *numericRegion, inside map[string]bool) bool {
	for _, r := range e.function.RecurrencesSnapshot() {
		if !e.budget.charge(1 + len(r.Initial) + len(r.Updates)) {
			return false
		}
		if !inside[r.Header] {
			continue
		}
		if p.header != "" && p.header != r.Header {
			return false
		}
		if r.Kind != facts.FlowKindNumeric {
			return false
		}
		p.header = r.Header
		p.slots = append(p.slots, r)
	}
	return p.header != ""
}
func numericRegionBlock(e *controlEvaluation, p *numericRegion, id string, inside map[string]bool, graph map[string][]string) bool {
	if !e.budget.charge(1) {
		return false
	}
	if !dominates(e.function, p.header, id) {
		return false
	}
	for _, in := range e.function.blocks[id].Block.Instructions {
		if !e.budget.charge(1) {
			return false
		}
		if in.Opcode == facts.OpReturn || in.Opcode == facts.OpThrow {
			return false
		}
	}
	graph[id] = nil
	edges := e.function.Successors(id)
	if len(edges) == 0 {
		return false
	}
	for _, edge := range edges {
		if !e.budget.charge(1) {
			return false
		}
		if edge.Kind != facts.EdgeNormal && edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
			return false
		}
		if !inside[edge.To] {
			if id != p.header || p.exit.To != "" {
				return false
			}
			p.exit = edge
		} else if edge.To != p.header {
			graph[id] = append(graph[id], edge.To)
		}
	}
	return true
}
func numericHeaderShape(e *controlEvaluation, p *numericRegion, inside map[string]bool) bool {
	edges := e.function.Successors(p.header)
	if len(edges) != 2 || p.exit.To == "" {
		return false
	}
	for _, edge := range edges {
		if inside[edge.To] {
			p.body = edge
		}
	}
	if p.body.Guard == "" || p.body.Guard != p.exit.Guard {
		return false
	}
	if !oppositeLoopEdges(p.body.Kind, p.exit.Kind) {
		return false
	}
	slots := map[string]bool{}
	for _, slot := range p.slots {
		slots[slot.PhiID] = true
	}
	for _, in := range e.function.blocks[p.header].Block.Instructions {
		if !e.budget.charge(1) {
			return false
		}
		switch in.Opcode {
		case facts.OpPhi:
			if !slots[in.ID] {
				return false
			}
		case facts.OpConstant, facts.OpBind, facts.OpPrimitive, facts.OpBranch, facts.OpCall:
		default:
			return false
		}
	}
	return true
}
func oppositeLoopEdges(a, b facts.EdgeKind) bool {
	return (a == facts.EdgeTrue && b == facts.EdgeFalse) || (a == facts.EdgeFalse && b == facts.EdgeTrue)
}
