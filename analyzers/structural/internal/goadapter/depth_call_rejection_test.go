package goadapter

import (
	"reflect"
	"testing"
)

func TestDepthRejectionHelperExtraction(t *testing.T) {
	direct := depthCallScore(t, `package p;func Calc(x int) int {if x<0 {panic("bad")};return x+1}`)
	for _, source := range []string{
		`package p;func check(x int) {if x<0 {panic("bad")}};func Calc(x int) int {check(x);return x+1}`,
		`package p;func checked(x int) int {if x<0 {panic("bad")};return x};func Calc(x int) int {checked(x);return x+1}`,
		`package p;func checked(x int) int {if x<0 {panic("bad")};return x+1};func Calc(x int) int {return checked(x)}`,
		`package p;func check(x int) {if x<0 {panic("bad")}};func forward(x int) {check(x)};func Calc(x int) int {forward(x);return x+1}`,
	} {
		helper := depthCallScore(t, source)
		if helper.Shallow == nil || direct.Shallow == nil || *helper.Shallow != *direct.Shallow || !reflect.DeepEqual(helper.Alternatives, direct.Alternatives) {
			t.Fatalf("extraction changed V/X: %s: %+v / %+v", source, direct, helper)
		}
		if len(helper.Dependencies) == 0 {
			t.Fatal("missing helper dependency")
		}
	}
}

func TestDepthRejectionHelperActualBindings(t *testing.T) {
	for _, test := range []struct {
		source string
		hidden uint64
	}{
		{`package p;func check(x int) {if x<0 {panic(x+100)}};func Calc(x,y int) int {check(y);return x+1}`, 2},
		{`package p;func check(x int) {if x<0 {panic("bad")}};func Calc(x int) int {check(1);return x+1}`, 2},
		{`package p;func check(b bool) {if b {panic("bad")}};func Calc(b bool) bool {check(b);return !b}`, 1},
	} {
		score := depthCallScore(t, test.source)
		if score.Shallow == nil || score.H != test.hidden {
			t.Fatalf("wrong bound rejection: %s: %+v", test.source, score)
		}
	}
}

func TestDepthUnsupportedRejectionCallerRemainsPartial(t *testing.T) {
	for _, source := range []string{
		`package p;func check(x int) {if x<0 {panic("bad")}};func Calc(n int) int {s:=0;for i:=0;i<n;i++ {check(i);s+=i};return s}`,
		`package p;func check(x int) {if x<0 {panic("bad")}};func Calc(x int) int {defer func(){}();check(x);return x}`,
	} {
		score := depthCallScore(t, source)
		if score.Shallow != nil {
			t.Fatalf("unsupported caller measured: %s: %+v", source, score)
		}
	}
}

func TestDepthShortCircuitPreservesRejectionBypass(t *testing.T) {
	for _, operator := range []string{"&&", "||"} {
		score := depthCallScore(t, `package p;func check(x int) bool {if x<0 {panic("bad")};return x>0};func Calc(b bool,x int) bool {return b`+operator+`check(x)}`)
		if score.Shallow == nil || score.H != 0 || score.Burden.P != 0 {
			t.Fatalf("short-circuit bypass invented universal V: %+v", score)
		}
	}
	for _, expression := range []string{"false&&check(x)", "true||check(x)"} {
		score := depthCallScore(t, `package p;func check(x int) bool {panic("bad")};func Calc(x int) bool {return `+expression+`}`)
		if score.Shallow == nil || score.H != 0 || len(score.Dependencies) != 0 {
			t.Fatalf("unreachable helper evaluated: %+v", score)
		}
	}
}
