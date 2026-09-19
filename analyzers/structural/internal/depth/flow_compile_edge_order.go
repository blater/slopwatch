package depth

import "slopslap.dev/structural/internal/facts"

func flowEdgeFields(e facts.FlowEdge) [7]string {
	return [7]string{e.To, string(e.Kind), e.Guard, e.GuardPolarity, e.Payload, e.ErrorTag, e.From}
}
func flowEdgeLess(a, b facts.FlowEdge) bool {
	left, right := flowEdgeFields(a), flowEdgeFields(b)
	for i, value := range left {
		if value != right[i] {
			return value < right[i]
		}
	}
	return false
}
func flowEdgeIdentity(edge facts.FlowEdge) string {
	var key []byte
	for _, field := range flowEdgeFields(edge) {
		key = appendString(key, field)
	}
	return string(key)
}
