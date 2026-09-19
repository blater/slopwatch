package depth

func controlComponents(c *CompiledFunction, charge func(int) bool) ([][]string, bool) {
	adjacency := make(map[string][]string, len(c.reachable))
	for _, id := range c.reachable {
		if !charge(1) {
			return nil, false
		}
		adjacency[id] = nil
		for _, edge := range c.Successors(id) {
			if !charge(1) {
				return nil, false
			}
			adjacency[id] = append(adjacency[id], edge.To)
		}
	}
	components, complete := stronglyConnected(adjacency, charge)
	if !complete {
		return nil, false
	}
	cycles := make([][]string, 0, len(components))
	for _, members := range components {
		if !charge(1) {
			return nil, false
		}
		if len(members) > 1 {
			cycles = append(cycles, members)
			continue
		}
		id := members[0]
		for _, target := range adjacency[id] {
			if !charge(1) {
				return nil, false
			}
			if id == target {
				cycles = append(cycles, members)
				break
			}
		}
	}
	return cycles, true
}

func rejectControlCycles(e *controlEvaluation) {
	if e.budget.status != "" {
		return
	}
	cycles, complete := controlComponents(e.function, e.budget.charge)
	if complete {
		for _, members := range cycles {
			e.gap(members[0], "", guardTrue, "unsupported_loop")
		}
	}
	e.result.Status = TransferUnsupported
}
