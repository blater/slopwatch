package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

type scoringCase struct {
	ID                 string                  `json:"id"`
	State              facts.KnowledgeState    `json:"state"`
	Burden             facts.Burden            `json:"burden"`
	Obligations        []facts.Obligation      `json:"obligations"`
	FamilyAlternatives [][]facts.ObligationSet `json:"family_alternatives"`
	RouteFamilies      []facts.RouteFamily     `json:"route_families"`
	Expected           struct {
		B8      uint64 `json:"B8"`
		H       uint64 `json:"H"`
		Shallow *int   `json:"shallow"`
	} `json:"expected"`
}

type scoringDocument struct {
	Cases       []scoringCase `json:"cases"`
	LedgerCases []ledgerCase  `json:"ledger_cases"`
}

type ledgerCase struct {
	BoundaryContributions  map[string]float64    `json:"boundary_contributions"`
	Layouts                []map[string][]string `json:"layouts"`
	ExpectedUniqueTotal    float64               `json:"expected_unique_total"`
	ExpectedProjectionSums []float64             `json:"expected_projection_sums"`
	ExpectedImprovement    bool                  `json:"expected_comparison_improvement"`
}

func loadScoringCases(t *testing.T) scoringDocument {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../.."))
	payload, err := os.ReadFile(filepath.Join(root, "docs/evidence/shallow-v4/scoring-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document scoringDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func boundaryFromCase(item scoringCase) facts.BoundaryAssessment {
	knowledge := map[string]facts.Knowledge{}
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		knowledge[dimension] = facts.Knowledge{State: item.State, Essential: true}
	}
	routeFamilies := append([]facts.RouteFamily(nil), item.RouteFamilies...)
	for index := range routeFamilies {
		if routeFamilies[index].ID == "" {
			routeFamilies[index].ID = fmt.Sprintf("fixture-family-%d", index)
		}
	}
	return facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "fixture", Audience: "public", View: "callable", Symbol: item.ID},
		State:    item.State, Knowledge: knowledge, Burden: item.Burden, Obligations: item.Obligations,
		FamilyAlternatives: item.FamilyAlternatives, RouteFamilies: routeFamilies,
	}
}

func TestDepthV4ConformsToCommittedScoringCases(t *testing.T) {
	document := loadScoringCases(t)
	if len(document.Cases) != 36 {
		t.Fatalf("fixture changed: got %d cases, want 36", len(document.Cases))
	}
	for _, item := range document.Cases {
		item := item
		t.Run(item.ID, func(t *testing.T) {
			boundary := boundaryFromCase(item)
			got, err := ScoreBoundary(boundary)
			if err != nil {
				t.Fatal(err)
			}
			if got.B8 != item.Expected.B8 || got.H != item.Expected.H {
				t.Fatalf("B8/H = %d/%d, want %d/%d", got.B8, got.H, item.Expected.B8, item.Expected.H)
			}
			if (got.Shallow == nil) != (item.Expected.Shallow == nil) {
				t.Fatalf("shallow nil = %v, want %v", got.Shallow == nil, item.Expected.Shallow == nil)
			}
			if got.Shallow != nil && *got.Shallow != *item.Expected.Shallow {
				t.Fatalf("shallow = %d, want %d", *got.Shallow, *item.Expected.Shallow)
			}
		})
	}
}

func TestDepthV4RouteSelectionIsCanonicalAndMonotone(t *testing.T) {
	base := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "d"}, State: facts.KnowledgeMeasured,
		Burden: facts.Burden{O: 1, T: 1}, Obligations: []facts.Obligation{{ID: "x", Category: facts.ObligationTransform}}, FamilyAlternatives: [][]facts.ObligationSet{{{"x"}}},
		RouteFamilies: []facts.RouteFamily{{ID: "f", Routes: []facts.Route{{ID: "z", RequiredSlots: []string{"value"}, ExposedSlots: []string{"value", "mode"}}, {ID: "a", RequiredSlots: []string{"value"}, ExposedSlots: []string{"value", "mode"}}}}},
	}
	base.Knowledge = measuredKnowledge()
	first, err := ScoreBoundary(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.SelectedRoutes) != 1 || first.SelectedRoutes[0] != "a" {
		t.Fatalf("route = %#v, want canonical tie winner a", first.SelectedRoutes)
	}
	base.RouteFamilies[0].Routes = append(base.RouteFamilies[0].Routes, facts.Route{ID: "default", RequiredSlots: []string{"value"}, ExposedSlots: []string{"value"}})
	second, err := ScoreBoundary(base)
	if err != nil {
		t.Fatal(err)
	}
	if second.B8 > first.B8 {
		t.Fatalf("default route worsened B8: %d -> %d", first.B8, second.B8)
	}
}

func measuredKnowledge() map[string]facts.Knowledge {
	result := map[string]facts.Knowledge{}
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		result[dimension] = facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
	}
	return result
}

func TestDepthV4RejectsMalformedAndUnknownFacts(t *testing.T) {
	base := facts.BoundaryAssessment{Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "d"}, State: facts.KnowledgeMeasured, Burden: facts.Burden{O: 1}, FamilyAlternatives: [][]facts.ObligationSet{{{"missing"}}}}
	base.Knowledge = measuredKnowledge()
	if _, err := ScoreBoundary(base); err == nil {
		t.Fatal("unknown obligation reference accepted")
	}
	base.Obligations = []facts.Obligation{{ID: "missing", Category: "bad"}}
	if _, err := ScoreBoundary(base); err == nil {
		t.Fatal("unknown obligation category accepted")
	}
	base.Obligations = nil
	base.FamilyAlternatives = [][]facts.ObligationSet{{nil}}
	base.Burden.O = -1
	if _, err := ScoreBoundary(base); err == nil {
		t.Fatal("negative burden accepted")
	}
	base.Burden.O = 1
	base.RouteFamilies = []facts.RouteFamily{{ID: "family", Routes: []facts.Route{{ID: "route", RequiredSlots: []string{"payload"}, ExposedSlots: []string{"other"}}}}}
	if _, err := ScoreBoundary(base); err == nil {
		t.Fatal("required slot accepted from sibling exposure")
	}
	base.RouteFamilies[0].Routes[0].ExposedSlots = []string{"payload"}
	base.RouteFamilies[0].Routes = append(base.RouteFamilies[0].Routes, facts.Route{ID: "route"})
	if _, err := ScoreBoundary(base); err == nil {
		t.Fatal("duplicate route accepted")
	}
}

func TestDepthV4EssentialDimensionsBlockWhenEssentialFlagOmitted(t *testing.T) {
	boundary := facts.BoundaryAssessment{Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "gap"}, State: facts.KnowledgeMeasured, Knowledge: measuredKnowledge(), Burden: facts.Burden{O: 1}, FamilyAlternatives: [][]facts.ObligationSet{{{}}}}
	boundary.Knowledge["behavior"] = facts.Knowledge{State: facts.KnowledgePartial}
	got, err := ScoreBoundary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shallow != nil || got.HKnown || got.State != facts.KnowledgePartial {
		t.Fatalf("essential gap scored: %#v", got)
	}
}

func TestDepthV4AlternativeCapWithholdsHAndScore(t *testing.T) {
	boundary := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "cap"}, State: facts.KnowledgeMeasured,
		Knowledge: measuredKnowledge(), Burden: facts.Burden{O: 1},
		FamilyAlternatives: [][]facts.ObligationSet{make([]facts.ObligationSet, 33)},
	}
	for index := range boundary.FamilyAlternatives[0] {
		id := "x" + string(rune('a'+index))
		boundary.FamilyAlternatives[0][index] = facts.ObligationSet{id}
		boundary.Obligations = append(boundary.Obligations, facts.Obligation{ID: id, Category: facts.ObligationTransform})
	}
	got, err := ScoreBoundary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != facts.KnowledgePartial || got.HKnown || got.Shallow != nil || len(got.Alternatives) != 0 {
		t.Fatalf("cap result = %#v", got)
	}
	for left, right := 0, len(boundary.FamilyAlternatives[0])-1; left < right; left, right = left+1, right-1 {
		boundary.FamilyAlternatives[0][left], boundary.FamilyAlternatives[0][right] = boundary.FamilyAlternatives[0][right], boundary.FamilyAlternatives[0][left]
	}
	permuted, err := ScoreBoundary(boundary)
	if err != nil || permuted.State != got.State || permuted.HKnown != got.HKnown || len(permuted.Alternatives) != len(got.Alternatives) {
		t.Fatalf("cap changed under permutation: %#v vs %#v", got, permuted)
	}
}

func TestDepthV4DeduplicatesSharedObligationsAndFamilyOrder(t *testing.T) {
	base := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "shared"}, State: facts.KnowledgeMeasured,
		Knowledge: measuredKnowledge(), Burden: facts.Burden{O: 2, T: 1, A: 2},
		Obligations:        []facts.Obligation{{ID: "state.C", Category: facts.ObligationState}, {ID: "out.X", Category: facts.ObligationTransform}},
		FamilyAlternatives: [][]facts.ObligationSet{{{"state.C"}, {"state.C", "out.X"}}, {{"state.C"}, {"out.X"}}},
	}
	first, err := ScoreBoundary(base)
	if err != nil {
		t.Fatal(err)
	}
	if first.H != 2 {
		t.Fatalf("shared H = %d, want 2", first.H)
	}
	base.FamilyAlternatives[0], base.FamilyAlternatives[1] = base.FamilyAlternatives[1], base.FamilyAlternatives[0]
	second, err := ScoreBoundary(base)
	if err != nil {
		t.Fatal(err)
	}
	if first.H != second.H || first.B8 != second.B8 {
		t.Fatalf("family permutation changed score inputs: %#v vs %#v", first, second)
	}
}

func TestDepthV4RejectsArithmeticOverflow(t *testing.T) {
	boundary := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "overflow"}, State: facts.KnowledgeMeasured,
		Knowledge: measuredKnowledge(), Burden: facts.Burden{O: math.MaxInt64}, FamilyAlternatives: [][]facts.ObligationSet{{{}}},
	}
	if _, err := ScoreBoundary(boundary); err == nil {
		t.Fatal("overflowing score accepted")
	}
}

func TestDepthV4NAPreservesInterfaceFactsAndValidatesThem(t *testing.T) {
	boundary := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "data"}, State: facts.KnowledgeNotApplicable,
		Burden: facts.Burden{O: 1, T: 2}, Concepts: []facts.Concept{{ID: "text", Kind: "text"}}, Reasons: []facts.Reason{{Code: "data_only"}},
	}
	got, err := ScoreBoundary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != facts.KnowledgeNotApplicable || got.Burden.O != 1 || got.Burden.T != 2 || got.Shallow != nil {
		t.Fatalf("N/A facts not preserved: %#v", got)
	}
	boundary.Obligations = []facts.Obligation{{ID: "bad", Category: "?"}}
	if _, err := ScoreBoundary(boundary); err == nil {
		t.Fatal("malformed N/A obligation accepted")
	}
	boundary.Obligations = nil
	boundary.RouteFamilies = []facts.RouteFamily{{ID: "f", Routes: []facts.Route{{ID: "r", ExposedSlots: []string{"payload"}, RequiredSlots: []string{"payload"}, RequiredPolicies: []string{"missing"}}}}}
	if _, err := ScoreBoundary(boundary); err == nil {
		t.Fatal("malformed N/A route policy accepted")
	}
}

func TestDepthV4AlternativeFamilyMappingIsComplete(t *testing.T) {
	boundary := facts.BoundaryAssessment{Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "mapping"}, State: facts.KnowledgeMeasured, Knowledge: measuredKnowledge(), Burden: facts.Burden{O: 2}, RouteFamilies: []facts.RouteFamily{{ID: "a", Routes: []facts.Route{{ID: "ra", ExposedSlots: []string{"x"}}}}, {ID: "b", Routes: []facts.Route{{ID: "rb", ExposedSlots: []string{"x"}}}}}, FamilyAlternatives: [][]facts.ObligationSet{{{}}, {{}}}, FamilyAlternativeIDs: []string{"a", "a"}}
	if _, err := ScoreBoundary(boundary); err == nil {
		t.Fatal("duplicate alternative family mapping accepted")
	}
	boundary.FamilyAlternativeIDs = []string{"a"}
	if _, err := ScoreBoundary(boundary); err == nil {
		t.Fatal("incomplete alternative family mapping accepted")
	}
	boundary.FamilyAlternativeIDs = []string{"a", "b"}
	boundary.Obligations = []facts.Obligation{{ID: "x", Category: facts.ObligationTransform}}
	boundary.FamilyAlternatives = [][]facts.ObligationSet{{{"x"}}, {{"x"}}}
	score, err := ScoreBoundary(boundary)
	if err != nil || score.H != 2 || score.FamilyHidden["a"] != 2 || score.FamilyHidden["b"] != 2 {
		t.Fatalf("family diagnostics lost shared responsibility: %+v, %v", score, err)
	}
}

func TestDepthV4RelevanceDoesNotCreateHiddenAlternatives(t *testing.T) {
	boundary := facts.BoundaryAssessment{
		Identity: facts.BoundaryIdentity{Artifact: "a", Audience: "b", View: "c", Symbol: "relevance"}, State: facts.KnowledgeMeasured,
		Knowledge: measuredKnowledge(), Burden: facts.Burden{O: 1}, FamilyAlternatives: [][]facts.ObligationSet{make([]facts.ObligationSet, 33)},
	}
	for index := range boundary.FamilyAlternatives[0] {
		id := fmt.Sprintf("u.%d", index)
		boundary.FamilyAlternatives[0][index] = facts.ObligationSet{id}
		boundary.Obligations = append(boundary.Obligations, facts.Obligation{ID: id, Category: facts.ObligationOutcome})
	}
	got, err := ScoreBoundary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != facts.KnowledgeMeasured || !got.HKnown || got.H != 0 || got.Shallow == nil || *got.Shallow != 100 {
		t.Fatalf("U alternatives affected score: %#v", got)
	}
}

func TestDepthV4LedgerFixtureArithmetic(t *testing.T) {
	document := loadScoringCases(t)
	if len(document.LedgerCases) != 1 {
		t.Fatalf("fixture ledger cases = %d, want 1", len(document.LedgerCases))
	}
	item := document.LedgerCases[0]
	unique := 0.0
	for _, contribution := range item.BoundaryContributions {
		unique += contribution
	}
	if unique != item.ExpectedUniqueTotal {
		t.Fatalf("unique total = %v, want %v", unique, item.ExpectedUniqueTotal)
	}
	if len(item.Layouts) != len(item.ExpectedProjectionSums) {
		t.Fatalf("layout fixture length mismatch")
	}
	for index, layout := range item.Layouts {
		projection := 0.0
		for _, boundaries := range layout {
			maximum := 0.0
			for _, boundaryID := range boundaries {
				if value := item.BoundaryContributions[boundaryID]; value > maximum {
					maximum = value
				}
			}
			projection += maximum
		}
		if projection != item.ExpectedProjectionSums[index] {
			t.Fatalf("layout %d projection = %v, want %v", index, projection, item.ExpectedProjectionSums[index])
		}
	}
	if item.ExpectedImprovement {
		t.Fatal("fixture unexpectedly declares regrouping improvement")
	}
}

func TestDepthV4InventoryFingerprintFreezesCallerSurface(t *testing.T) {
	boundary := facts.BoundaryAssessment{
		Identity:      facts.BoundaryIdentity{Artifact: "service", Audience: "external", View: "module", Symbol: "service"},
		State:         facts.KnowledgeMeasured,
		Knowledge:     measuredKnowledge(),
		Concepts:      []facts.Concept{{ID: "number", Kind: "number"}},
		Slots:         []facts.Slot{{ID: "route/arg0", Concept: "number", Required: true}},
		RouteFamilies: []facts.RouteFamily{{ID: "route", Routes: []facts.Route{{ID: "route", Family: "route", Signature: "(number)->number", RequiredSlots: []string{"route/arg0"}, ExposedSlots: []string{"route/arg0"}}}}},
	}
	first := inventoryFingerprint(boundary)
	if first == "" {
		t.Fatal("measured caller surface has no inventory fingerprint")
	}
	bodyEdit := boundary
	bodyEdit.Evidence = []facts.Evidence{{ID: "changed-body-proof", Description: "implementation detail"}}
	if got := inventoryFingerprint(bodyEdit); got != first {
		t.Fatalf("body/evidence edit changed inventory: %q != %q", got, first)
	}
	signatureEdit := boundary
	signatureEdit.RouteFamilies[0].Routes[0].Signature = "(text)->number"
	if got := inventoryFingerprint(signatureEdit); got == first {
		t.Fatal("signature edit did not change inventory")
	}
	applicabilityEdit := boundary
	applicabilityEdit.State = facts.KnowledgeNotApplicable
	if got := inventoryFingerprint(applicabilityEdit); got == first {
		t.Fatal("applicability edit did not change inventory")
	}
}
