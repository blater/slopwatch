package report

import "testing"

func TestDisplayNumberMatchesReferenceSignificantDigits(t *testing.T) {
	if got := DisplayNumber(40.196081981981); got != "40.1961" {
		t.Fatalf("DisplayNumber() = %q, want %q", got, "40.1961")
	}
	if got := OneDecimal(82); got != "82.0" {
		t.Fatalf("OneDecimal() = %q, want %q", got, "82.0")
	}
}

func TestSortAndRankUsesScoreThenPath(t *testing.T) {
	document := Document{Files: []File{
		{Path: "z.go", Score: 2}, {Path: "b.go", Score: 5}, {Path: "a.go", Score: 5},
	}}
	document.SortAndRank()
	want := []string{"a.go", "b.go", "z.go"}
	for index, path := range want {
		if document.Files[index].Path != path || document.Files[index].Rank != index+1 {
			t.Fatalf("rank %d = %#v, want %s", index+1, document.Files[index], path)
		}
	}
}
