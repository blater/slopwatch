package depth

// Condensation orders loops before their consumers regardless of block spelling.
// Backedges remain real edges: evaluating a latch never fabricates a completion.
func evaluateControlRegions(e *controlEvaluation, initial *TransferState) FlowEvaluation {
	adjacency, ok := controlAdjacency(e.function, e.budget.charge)
	if !ok {
		return e.finish()
	}
	components, ok := stronglyConnected(adjacency, e.budget.charge)
	if !ok {
		return e.finish()
	}
	order, members := controlRegionOrder(components, adjacency, e.budget.charge)
	e.inputs[e.function.function.Entry] = []controlInput{{guard: guardTrue, state: initial}}
	for _, id := range order {
		if !e.budget.charge(1) {
			break
		}
		region := members[id]
		if len(region) != 1 || containsControlTarget(adjacency[id], id) {
			evaluateNumericRegion(e, region)
		} else {
			evaluateControlBlock(e, id)
		}
		if e.budget.status != "" {
			break
		}
	}
	return e.finish()
}
func evaluateControlBlock(e *controlEvaluation, id string) {
	incoming := e.inputs[id]
	if len(incoming) == 0 {
		return
	}
	frame := e.merge(id, incoming)
	if e.budget.status != "" {
		return
	}
	e.result.Blocks++
	e.block(id, frame, incoming)
	delete(e.inputs, id)
}
func containsControlTarget(targets []string, target string) bool {
	for _, id := range targets {
		if id == target {
			return true
		}
	}
	return false
}
func controlRegionOrder(components [][]string, adjacency map[string][]string, charge func(int) bool) ([]string, map[string][]string) {
	owners, members := map[string]string{}, map[string][]string{}
	for _, component := range components {
		if !charge(len(component)) {
			return nil, members
		}
		members[component[0]] = component
		for _, id := range component {
			owners[id] = component[0]
		}
	}
	graph := map[string][]string{}
	for _, component := range components {
		id := component[0]
		graph[id] = nil
		for _, block := range component {
			for _, target := range adjacency[block] {
				if !charge(1) {
					return nil, members
				}
				if owners[target] != id {
					graph[id] = append(graph[id], owners[target])
				}
			}
		}
	}
	order, _ := orderControlGraph(graph, charge)
	return order, members
}
