package sourceestimate

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestRustTraitMatcherPreservesSubstringMatches(t *testing.T) {
	names := map[string]bool{"Read": true, "Reader": true, "ead": true, "ad": true, "Send": true, "é": true, "Missing": true}
	for _, signature := range []string{"", "impl Reader + Send", "Box<ReaderReader>", "PréReader", "ead", "Unrelated"} {
		assertRustTraitMatches(t, names, signature)
	}
}

func TestRustTraitMatcherMatchesContainsReference(t *testing.T) {
	random := rand.New(rand.NewSource(71))
	word := func(length int) string {
		var result strings.Builder
		for i := 0; i < length; i++ {
			result.WriteByte("abc"[random.Intn(3)])
		}
		return result.String()
	}
	for iteration := 0; iteration < 40; iteration++ {
		names := map[string]bool{}
		for i := 0; i < 80; i++ {
			names[word(1+random.Intn(12))] = true
		}
		for i := 0; i < 20; i++ {
			assertRustTraitMatches(t, names, word(random.Intn(100)))
		}
	}
}

func assertRustTraitMatches(t *testing.T, names map[string]bool, signature string) {
	t.Helper()
	got, want := map[string]bool{}, map[string]bool{}
	newRustTraitMatcher(names).match(signature, func(name string) { got[name] = true })
	for name := range names {
		if strings.Contains(signature, name) {
			want[name] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("signature %q: got %v, want %v", signature, got, want)
	}
}

func TestRustTraitEscapesRetainsPackagesImportsAndVisibility(t *testing.T) {
	units := rustInventoryTestUnits(
		"trait Read {} trait Private {} struct X; impl Imported for X { fn run(&self) {} }",
		"trait Foreign {}",
		"trait Read {}",
	)
	units[0].pkg, units[1].pkg, units[2].pkg = "a", "b", "c"
	units[0].ops = []*operation{
		{exposed: true, returnType: "impl Reader + Imported + Foreign"},
		{exposed: false, returnType: "Private"},
	}
	units[1].ops = []*operation{{exposed: true, returnType: "Foreign"}}
	got := rustTraitEscapes(units)
	want := map[string]bool{"a#Read": true, "a#Imported": true, "b#Foreign": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trait escapes = %v, want %v", got, want)
	}
}

func TestRustTraitMatcherRetiresMatchedSuffixes(t *testing.T) {
	names := map[string]bool{"Read": true, "ead": true, "ad": true, "d": true, "Send": true}
	matcher := newRustTraitMatcher(names)
	counts := map[string]int{}
	for _, signature := range []string{"ReadRead", "Read", "SendRead", "Send"} {
		matcher.match(signature, func(name string) { counts[name]++ })
	}
	for name := range names {
		if counts[name] != 1 {
			t.Fatalf("%q emitted %d times", name, counts[name])
		}
	}
}
