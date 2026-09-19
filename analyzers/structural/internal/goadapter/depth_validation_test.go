package goadapter

import (
	"strings"
	"testing"
)

func TestDepthInputRejectionValidation(t *testing.T) {
	for _, test := range []struct {
		name, source string
		hidden       uint64
		validation   bool
	}{
		{"guardMakesConstant", `package p;func Calc(b bool) bool {if b {panic("bad")};return !b}`, 1, true},
		{"reflexiveDeadGuard", `package p;func Calc(x int) int {if x<x {panic("bad")};return x}`, 0, false},
		{"validatedTransform", `package p;func Calc(x int) int {if x<0 {panic("bad")};return x+1}`, 3, true},
		{"validatedIdentity", `package p;func Calc(x int) int {if x<0 {panic("bad")};return x}`, 1, true},
		{"unrelatedGuard", `package p;func Calc(x,y int) int {if y<0 {panic("bad")};return x+1}`, 2, false},
		{"repeatedGuard", `package p;func Calc(x int) int {if x<0 {panic("bad")};if x<0 {panic("again")};return x+1}`, 3, true},
		{"deadRejection", `package p;func Calc(x int) int {if false {panic("bad")};return x+1}`, 2, false},
		{"shadowedBuiltin", `package p;func panic(x string) {};func Calc(x int) int {if x<0 {panic("bad")};return x+1}`, 2, false},
		{"messageNotTransformation", `package p;func Calc(x int) int {if x<0 {panic(x+100)};return x}`, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			score := depthCallScore(t, test.source)
			if score.Shallow == nil || score.H != test.hidden {
				t.Fatalf("wrong responsibilities: %+v", score)
			}
			found := false
			for _, alternative := range score.Alternatives {
				for _, id := range alternative {
					found = found || strings.HasPrefix(id, "V:")
				}
			}
			if found != test.validation {
				t.Fatalf("wrong V evidence: %+v", score)
			}
		})
	}
}

func TestDepthUnsupportedRejectionDoesNotInventGuarantees(t *testing.T) {
	for _, source := range []string{
		`package p;func Calc(x int) int {panic("bad")}`,
		`package p;var state int;func fail(x int) int {state=x;return x};func Calc(x int) int {if x<0 {panic(fail(x))};return x}`,
	} {
		score := depthCallScore(t, source)
		if score.Shallow != nil {
			t.Fatalf("unsupported guarantee measured: %s: %+v", source, score)
		}
	}
}
