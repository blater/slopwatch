package depth

type controlReady []string

func (q controlReady) Len() int           { return len(q) }
func (q controlReady) Less(i, j int) bool { return q[i] < q[j] }
func (q controlReady) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *controlReady) Push(v any)        { *q = append(*q, v.(string)) }
func (q *controlReady) Pop() any          { old := *q; n := len(old) - 1; v := old[n]; *q = old[:n]; return v }

// topologicalControlOrder counts edges, including parallel edges, and admits
// each block once. The compiler already removed ordinary post-return edges.
func topologicalControlOrder(c *CompiledFunction, charge func(int) bool) ([]string, bool) {
	graph, ok := controlAdjacency(c, charge)
	if !ok {
		return nil, false
	}
	return orderControlGraph(graph, charge)
}
