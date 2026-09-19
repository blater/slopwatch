package goadapter

import (
	"os"
	"path/filepath"
	"reflect"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func depthCallScore(t *testing.T, source string) metrics.DepthScore {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 {
		t.Fatal(scores)
	}
	return scores[0]
}
func TestDepthHelperExtractionPreservesScoreAndObligation(t *testing.T) {
	pairs := [][2]string{
		{`package p;func Compute(x,y int) int {return x-y}`, `package p;func subtract(a,b int) int {return a-b};func Compute(x,y int) int {return subtract(x,y)}`},
		{`package p;func Compute(x,y int) int {return y-x}`, `package p;func subtract(a,b int) int {return a-b};func Compute(x,y int) int {return subtract(y,x)}`},
		{`package p;func Compute(n int) int {s:=0;for i:=0;i<n;i++ {s+=i};return s}`, `package p;func add(a,b int) int {return a+b};func Compute(n int) int {s:=0;for i:=0;i<n;i++ {s=add(s,i)};return s}`},
		{`package p;func Compute(n int) int {s:=0;for i:=0;i<n;i++ {s+=i};return s}`, `package p;func sum(n int) int {s:=0;for i:=0;i<n;i++ {s+=i};return s};func Compute(n int) int {return sum(n)}`},
		{`package p;func Compute(n int) int {s:=0;for i:=0;i<n;i++ {s+=i};return s}`, `package p;func less(a,b int) bool {return a<b};func Compute(n int) int {s:=0;for i:=0;less(i,n);i++ {s+=i};return s}`},
	}
	for index, pair := range pairs {
		direct, helper := depthCallScore(t, pair[0]), depthCallScore(t, pair[1])
		if direct.Shallow == nil || helper.Shallow == nil || *direct.Shallow != *helper.Shallow || direct.H != helper.H || !reflect.DeepEqual(direct.Alternatives, helper.Alternatives) {
			t.Fatalf("pair %d: direct %+v helper %+v", index, direct, helper)
		}
	}
}
func TestDepthHelperEffectsAndUnresolvedTargetsRemainPartial(t *testing.T) {
	sources := []string{
		`package p;var state int;func write(x int) int {state=x;return 0};func Compute(x int) int {_=write(x);return x}`,
		`package p;var state int;func write(x int) {state=x};func Compute(x int) int {write(x);return x}`,
		`package p;func divide(a,b int) int {return a/b};func Compute(x int) int {_=divide(x,x);return x}`,
		`package p;func again(x int) int {return again(x)};func Compute(x int) int {return again(x)}`,
		`package p;func one(x int) int {return two(x)};func two(x int) int {return one(x)};func Compute(x int) int {return one(x)}`,
		`package p;func helper(x int) int {return x};func Compute(x int) int {f:=helper;return f(x)}`,
		`package p;func sum(n int) int {s:=0;for i:=0;i<n;i++ {s+=i};return s};func Compute(n int) int {s:=0;for i:=0;i<n;i++ {s+=sum(i)};return s}`,
	}
	for _, source := range sources {
		score := depthCallScore(t, source)
		if score.Shallow != nil {
			t.Fatalf("unsupported helper scored: %s: %+v", source, score)
		}
	}
}
func TestDepthPureDiscardedHelperAddsNoResponsibility(t *testing.T) {
	score := depthCallScore(t, `package p;func work(x int) int {return x+1};func Compute(x int) int {_=work(x);return x}`)
	if score.Shallow == nil || *score.Shallow != 100 || score.H != 0 {
		t.Fatal(score)
	}
}

func TestDepthRetainsDiscardedAndVoidCallDependencies(t *testing.T) {
	score := depthCallScore(t, `package p;func leaf(x int) int {return x+1};func drop(x int) {_=leaf(x)};func Compute(x int) int {drop(x);return x}`)
	if score.Shallow == nil || *score.Shallow != 100 || !reflect.DeepEqual(score.Dependencies, []string{".|p/drop", ".|p/leaf"}) {
		t.Fatalf("lost unused-call dependency: %+v", score)
	}
}
