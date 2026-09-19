package goadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestDepthDefaultForwardingRoutesShareFamilyAndNormalizeBurden(t *testing.T) {
	score := depthCallScore(t, `package p;func core(x int,mode bool)int{if mode{return x+1};return x*2};func Configured(x int,mode bool)int{return core(x,mode)};func Default(x int)int{return core(x,false)}`)
	if score.Shallow == nil || *score.Shallow != 37 || score.H != 2 {
		t.Fatalf("default route score = %+v", score)
	}
	if score.Burden.O != 1 || score.Burden.A != 1 || score.Burden.E != 1 || score.Burden.P != 0 || len(score.SelectedRoutes) != 1 {
		t.Fatalf("default route burden = %+v", score)
	}
}

func TestDepthCheckedAndRawRoutesShareFamilyWithConditionalValidation(t *testing.T) {
	score := depthCallScore(t, `package p;import "errors";func core(x int)int{return x+1};func Checked(x int)(int,error){if x<0{return 0,errors.New("negative")};return core(x),nil};func Raw(x int)int{return core(x)}`)
	if score.Shallow == nil || score.H != 2 || score.Burden.O != 1 {
		t.Fatalf("checked/raw score = %+v", score)
	}
	if len(score.Alternatives) != 2 {
		t.Fatalf("checked/raw alternatives = %+v", score.Alternatives)
	}
	var withValidation, withoutValidation bool
	for _, alternative := range score.Alternatives {
		text := strings.Join([]string(alternative), "\x00")
		withValidation = withValidation || strings.Contains(text, string(facts.ObligationValidation)+":")
		withoutValidation = withoutValidation || !strings.Contains(text, string(facts.ObligationValidation)+":")
	}
	if !withValidation || !withoutValidation {
		t.Fatalf("checked/raw validation was not conditional: %+v", score.Alternatives)
	}
	if score.Burden.P != 0 {
		t.Fatalf("checked/raw inherited route policy: %+v", score.Burden)
	}
}

func TestDepthNonForwardingRoutesStayConservative(t *testing.T) {
	cases := []struct {
		name, source, family string
		a                    int64
	}{
		{"transformed result", `package p;func helper(x int)int{return x+1};func Calc(x int)int{return helper(x)+1}`, ".|p/Calc", 1},
		{"nested argument", `package p;func core(x int)int{return x+1};func other(x int)int{return x};func Calc(x int)int{return core(other(x))}`, ".|p/Calc", 1},
		{"side effect before return", `package p;var state int;func work(x int){state=x};func Calc(x int)int{work(x);return x}`, ".|p/Calc", 1},
		{"non-nil companion", `package p;import "errors";func core(x int)int{return x+1};func unrelated()error{return errors.New("x")};func Calc(x int)(int,error){return core(x),unrelated()}`, ".|p/Calc", 1},
		{"duplicate binding", `package p;func core(x,y int)int{return x+y};func Calc(x int)int{return core(x,x)}`, ".|p/core", 1},
		{"alternate target", `package p;func left(x int)int{return x+1};func right(x int)int{return x*2};func Calc(mode bool,x int)int{if mode{return left(x)};return right(x)}`, ".|p/Calc", 2},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(item.source), 0600); err != nil {
				t.Fatal(err)
			}
			program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			families := program.Depth.Boundaries[0].RouteFamilies
			if len(families) != 1 || families[0].ID != item.family {
				t.Fatalf("families = %+v", families)
			}
			score := metrics.MeasureDepth(program)[0]
			if score.Burden.A != item.a {
				t.Fatalf("burden = %+v", score.Burden)
			}
		})
	}
}
