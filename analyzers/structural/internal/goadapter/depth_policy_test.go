package goadapter

import (
	"reflect"
	"testing"
)

func TestDepthConditionalPolicyVersusData(t *testing.T) {
	cases := []struct {
		name, source string
		policy       int64
		hidden       uint64
		score        int
	}{
		{"data", `package p;func Encode(b bool) int {if b {return 1};return 0}`, 0, 2, 36},
		{"policy", `package p;func Calc(mode bool,x int) int {if mode {return x+1};return x*2}`, 1, 2, 47},
		{"identityAlternative", `package p;func Calc(mode bool,x int) int {if mode {return x};return x+1}`, 1, 0, 100},
		{"sameBranch", `package p;func Calc(mode bool,x int) int {if mode {return x};return x}`, 0, 0, 100},
		{"payloadPredicate", `package p;func Calc(x int) int {if x>0 {return x+1};return x*2}`, 0, 2, 30},
		{"phi", `package p;func Calc(mode bool,x int) int {y:=0;if mode {y=x+1}else{y=x*2};return y}`, 1, 2, 47},
		{"boolIdentity", `package p;func Encode(b bool) bool {if b {return true};return false}`, 0, 0, 100},
		{"constantActual", `package p;func calc(mode bool,x int) int {if mode {return x+1};return x*2};func Calc(x int) int {return calc(true,x)}`, 0, 2, 30},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			score := depthCallScore(t, test.source)
			if score.Shallow == nil || *score.Shallow != test.score || score.H != test.hidden || score.Burden.P != test.policy {
				t.Fatalf("wrong semantics: %+v", score)
			}
		})
	}
}
func TestDepthPolicySurvivesHelperExtraction(t *testing.T) {
	direct := depthCallScore(t, `package p;func Calc(mode bool,x int) int {if mode {return x+1};return x*2}`)
	helper := depthCallScore(t, `package p;func choose(flag bool,payload int) int {if flag {return payload+1};return payload*2};func Calc(mode bool,x int) int {return choose(mode,x)}`)
	if direct.Shallow == nil || helper.Shallow == nil || *direct.Shallow != *helper.Shallow || direct.Burden != helper.Burden || !reflect.DeepEqual(direct.Alternatives, helper.Alternatives) {
		t.Fatalf("helper changed policy: %+v / %+v", direct, helper)
	}
}

func TestDepthLoopPolicyRemainsExplicitlyIncomplete(t *testing.T) {
	score := depthCallScore(t, `package p;func Calc(mode bool,n int) int {s:=0;for i:=0;i<n;i++ {if mode {s+=i}else{s+=i*2}};return s}`)
	if score.Shallow != nil {
		t.Fatalf("loop policy omitted from burden: %+v", score)
	}
}

func TestDepthCompoundPolicyGuardsPreserveIdentityAlternative(t *testing.T) {
	cases := []struct {
		source   string
		policies int64
		hidden   uint64
	}{
		{`package p;func Calc(a,b bool,x int) int {if a {if b{return x};return x+1};return x+2}`, 2, 0},
		{`package p;func Calc(a,b bool,x int) int {if a {if b{return x*2};return x+1};return x+2}`, 2, 2},
		{`package p;func Calc(a bool,x int) int {if a==true{return x};return x+1}`, 1, 0},
		{`package p;func Calc(a bool,x int) int {if false!=a{return x};return x+1}`, 1, 0},
		{`package p;func Calc(a,b bool,x int) int {if a&&b{return x};return x+1}`, 2, 0},
	}
	for _, test := range cases {
		score := depthCallScore(t, test.source)
		if score.Shallow == nil || score.H != test.hidden || score.Burden.P != test.policies {
			t.Fatalf("lost selector: %s: %+v", test.source, score)
		}
	}
}

func TestDepthInvariantLoopConditionCannotInventTransformation(t *testing.T) {
	score := depthCallScore(t, `package p;func Calc(run bool,x int) int {for run {x++};return x}`)
	if score.Shallow != nil {
		t.Fatalf("nonterminating branch earned X: %+v", score)
	}
}

func TestDepthBooleanReturnIdentity(t *testing.T) {
	for _, expression := range []string{"b", "b==true", "true==b", "b!=false", "false!=b", "!!b", "b&&true", "b||false"} {
		for _, source := range []string{
			"package p;func Encode(b bool) bool {return " + expression + "}",
			"package p;func encode(b bool) bool {return " + expression + "};func Encode(b bool) bool {return encode(b)}",
		} {
			score := depthCallScore(t, source)
			if score.Shallow == nil || *score.Shallow != 100 || score.H != 0 {
				t.Fatalf("identity %s earned responsibility: %+v", source, score)
			}
		}
	}
	for _, expression := range []string{"!b", "b==false", "false==b", "b!=true", "true!=b"} {
		score := depthCallScore(t, "package p;func Encode(b bool) bool {return "+expression+"}")
		if score.Shallow == nil || score.H != 2 {
			t.Fatalf("negation %s lost transformation: %+v", expression, score)
		}
	}
}
