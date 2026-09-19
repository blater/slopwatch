package goadapter

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDepthConditionalValidationAlternatives(t *testing.T) {
	sources := []string{
		`package p;func Calc(mode bool,x int) int {if mode {if x<0 {panic("bad")}};return x+1}`,
		`package p;func Calc(mode bool,x int) int {if !mode {return x+1};if x<0 {panic("bad")};return x+1}`,
		`package p;func check(x int) {if x<0 {panic("bad")}};func Calc(mode bool,x int) int {if mode {check(x)};return x+1}`,
	}
	var alternatives [][]string
	for _, source := range sources {
		score := depthCallScore(t, source)
		if score.Shallow == nil || score.H != 2 || score.Burden.P != 0 || len(score.Alternatives) != 2 {
			t.Fatalf("bypass not preserved: %s: %+v", source, score)
		}
		withV, withoutV := 0, 0
		for _, set := range score.Alternatives {
			validation := false
			for _, id := range set {
				validation = validation || strings.HasPrefix(id, "V:")
			}
			if validation {
				withV++
			} else {
				withoutV++
			}
		}
		if withV != 1 || withoutV != 1 {
			t.Fatalf("wrong V alternatives: %+v", score)
		}
		actual := make([][]string, len(score.Alternatives))
		for i, set := range score.Alternatives {
			actual[i] = []string(set)
		}
		if alternatives != nil && !reflect.DeepEqual(alternatives, actual) {
			t.Fatalf("extraction changed alternatives: %v / %v", alternatives, actual)
		}
		alternatives = actual
	}
}

func TestDepthValidationSelectorKeepsPolicyCorrelation(t *testing.T) {
	score := depthCallScore(t, `package p;func Calc(mode bool,x int) int {if mode {if x<0 {panic("bad")};return x};return x+1}`)
	if score.Shallow == nil || score.H != 1 || score.Burden.P != 1 {
		t.Fatalf("selector split lost correlation or burden: %+v", score)
	}
	// Boolean data that is itself validated is not a bypass selector.
	data := depthCallScore(t, `package p;func Calc(valid bool) bool {if !valid {panic("bad")};return valid}`)
	if data.Shallow == nil || data.H != 1 || data.Burden.P != 0 {
		t.Fatalf("data validation split away: %+v", data)
	}
}

func TestDepthConditionalValidationDeduplicatesBeforeCap(t *testing.T) {
	var parameters, checks []string
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("b%d", i)
		parameters = append(parameters, name+" bool")
		checks = append(checks, "if "+name+" {if x<0 {panic(0)}}")
	}
	source := "package p;func Calc(" + strings.Join(parameters, ",") + ",x int) int {" + strings.Join(checks, ";") + ";return x+1}"
	score := depthCallScore(t, source)
	if score.Shallow == nil || score.H != 2 || len(score.Alternatives) != 2 {
		t.Fatalf("equivalent validation switches expanded: %+v", score)
	}
}

func TestDepthConditionalValidationAlternativeLimit(t *testing.T) {
	var parameters, checks []string
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("b%d", i)
		parameters = append(parameters, name+" bool")
		checks = append(checks, fmt.Sprintf("if %s {if x<%d {panic(0)}}", name, i))
	}
	source := "package p;func Calc(" + strings.Join(parameters, ",") + ",x int) int {" + strings.Join(checks, ";") + ";return x+1}"
	score := depthCallScore(t, source)
	if score.Shallow != nil || !strings.Contains(fmt.Sprint(score.Reasons), "alternative_limit") {
		t.Fatalf("cap not explicit: %+v", score)
	}
}
