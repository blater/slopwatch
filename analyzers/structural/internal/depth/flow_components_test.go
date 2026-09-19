package depth

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func TestComponentsAgainstReachability(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for sample := 0; sample < 200; sample++ {
		size := 1 + rng.Intn(20)
		graph := map[string][]string{}
		reach := make([][]bool, size)
		for i := 0; i < size; i++ {
			reach[i] = make([]bool, size)
			reach[i][i] = true
			graph[fmt.Sprint(i)] = nil
			for j := 0; j < size; j++ {
				if rng.Intn(6) == 0 {
					graph[fmt.Sprint(i)] = append(graph[fmt.Sprint(i)], fmt.Sprint(j))
					reach[i][j] = true
				}
			}
		}
		for k := 0; k < size; k++ {
			for i := 0; i < size; i++ {
				for j := 0; j < size; j++ {
					reach[i][j] = reach[i][j] || reach[i][k] && reach[k][j]
				}
			}
		}
		expected := [][]string{}
		seen := map[int]bool{}
		for i := 0; i < size; i++ {
			if seen[i] {
				continue
			}
			group := []string{}
			for j := 0; j < size; j++ {
				if reach[i][j] && reach[j][i] {
					seen[j] = true
					group = append(group, fmt.Sprint(j))
				}
			}
			sort.Strings(group)
			expected = append(expected, group)
		}
		sort.Slice(expected, func(i, j int) bool { return expected[i][0] < expected[j][0] })
		work := 0
		actual, ok := stronglyConnected(graph, func(n int) bool { work += n; return true })
		if !ok || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("sample %d: got %v want %v", sample, actual, expected)
		}
		for _, edges := range graph {
			for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
				edges[i], edges[j] = edges[j], edges[i]
			}
		}
		reversed, ok := stronglyConnected(graph, func(int) bool { return true })
		if !ok || !reflect.DeepEqual(actual, reversed) {
			t.Fatal("edge order affected SCC partition")
		}
		for limit := 0; limit < work; limit++ {
			remaining := limit
			partial, complete := stronglyConnected(graph, func(n int) bool { remaining -= n; return remaining >= 0 })
			if complete || partial != nil {
				t.Fatalf("cutoff %d exposed partial partition", limit)
			}
		}
	}
}
func TestComponentsDeepChain(t *testing.T) {
	graph := map[string][]string{}
	const count = 10000
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("%05d", i)
		graph[id] = nil
		if i+1 < count {
			graph[id] = []string{fmt.Sprintf("%05d", i+1)}
		}
	}
	work := 0
	components, complete := stronglyConnected(graph, func(n int) bool { work += n; return true })
	if !complete || len(components) != count || work > 5*count {
		t.Fatalf("chain: %d components %d work", len(components), work)
	}
}
