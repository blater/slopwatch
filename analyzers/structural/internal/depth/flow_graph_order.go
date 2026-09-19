package depth

import "container/heap"

func orderControlGraph(graph map[string][]string, charge func(int) bool) ([]string, bool) {
	degrees := map[string]int{}
	ready := &controlReady{}
	for id, targets := range graph {
		if !charge(1 + len(targets)) {
			return nil, false
		}
		if _, ok := degrees[id]; !ok {
			degrees[id] = 0
		}
		for _, target := range targets {
			degrees[target]++
		}
	}
	for id, count := range degrees {
		if count == 0 {
			heap.Push(ready, id)
		}
	}
	var order []string
	for ready.Len() > 0 {
		if !charge(1 + indexSearchWork(ready.Len())) {
			return nil, false
		}
		id := heap.Pop(ready).(string)
		order = append(order, id)
		for _, target := range graph[id] {
			if !charge(1) {
				return nil, false
			}
			degrees[target]--
			if degrees[target] == 0 {
				heap.Push(ready, target)
			}
		}
	}
	return order, len(order) == len(graph)
}

func controlAdjacency(c *CompiledFunction, charge func(int) bool) (map[string][]string, bool) {
	graph := map[string][]string{}
	for _, id := range c.reachable {
		if !charge(1) {
			return nil, false
		}
		graph[id] = nil
		for _, edge := range c.Successors(id) {
			if !charge(1) {
				return nil, false
			}
			graph[id] = append(graph[id], edge.To)
		}
	}
	return graph, true
}
