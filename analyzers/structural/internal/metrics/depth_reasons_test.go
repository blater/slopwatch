package metrics

import (
	"reflect"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestCanonicalReasonsDeduplicatesWithoutLosingWitnesses(t *testing.T) {
	input := []facts.Reason{
		{Code: "unknown", Dimension: "behavior", Message: "call", FactIDs: []string{"b", "a"}},
		{Code: "unknown", Dimension: "behavior", Message: "call", FactIDs: []string{"c", "a"}},
		{Code: "unknown", Dimension: "behavior", Message: "field"},
		{Code: "unknown", Dimension: "inventory", Message: "call"},
	}
	got := canonicalReasons(input)
	if len(got) != 3 || !reflect.DeepEqual(got[0].FactIDs, []string{"a", "b", "c"}) {
		t.Fatalf("lost distinct reason or witness: %+v", got)
	}
	if !reflect.DeepEqual(input[0].FactIDs, []string{"b", "a"}) {
		t.Fatal("input witnesses mutated")
	}
	for left, right := 0, len(input)-1; left < right; left, right = left+1, right-1 {
		input[left], input[right] = input[right], input[left]
	}
	if !reflect.DeepEqual(got, canonicalReasons(input)) {
		t.Fatal("reason ordering depends on route traversal")
	}
}
