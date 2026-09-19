package goadapter

import (
	"os"
	"path/filepath"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func TestDepthStandardErrorValidation(t *testing.T) {
	for _, source := range []string{
		`package p;import "errors";func Calc(x int)(int,error){if x<0{return 0,errors.New("bad")};return x+1,nil}`,
		`package p;import e "errors";func Calc(x int)(int,error){if x<0{return 0,e.New("bad")};return x+1,nil}`,
		`package p;import . "errors";func Calc(x int)(int,error){if x<0{return 0,New("bad")};return x+1,nil}`,
	} {
		score := depthCallScore(t, source)
		if score.Shallow == nil || score.H != 3 || score.Burden.T != 2 {
			t.Fatalf("error validation unavailable: %s: %+v", source, score)
		}
	}
}

func TestDepthPassiveErrorsDoNotEarnTransformation(t *testing.T) {
	score := depthCallScore(t, `package p;import "errors";func Calc(x int)int{_=errors.New("unused");return x}`)
	if score.Shallow == nil || score.H != 0 {
		t.Fatalf("passive error earned X: %+v", score)
	}
	panicScore := depthCallScore(t, `package p;import "errors";func Calc(x int)int{if x<0{panic(errors.New("bad"))};return x}`)
	if panicScore.Shallow == nil || panicScore.H != 1 {
		t.Fatalf("error allocation interfered with V: %+v", panicScore)
	}
}

func TestDepthErrorContractsRemainExact(t *testing.T) {
	for _, source := range []string{
		`package p;import "errors";func Calc(x int)(int,error){factory:=errors.New;if x<0{return 0,factory("bad")};return x,nil}`,
		`package p;import "errors";var state int;func message(x int)string{state=x;return "bad"};func Calc(x int)(int,error){if x<0{return 0,errors.New(message(x))};return x,nil}`,
		`package p;import "errors";func Calc(x int)(int,error){if x<0{return 0,errors.Unwrap(errors.New("bad"))};return x,nil}`,
	} {
		score := depthCallScore(t, source)
		if score.Shallow != nil {
			t.Fatalf("unknown error contract measured: %s: %+v", source, score)
		}
	}
}

func TestDepthErrorFlowRetainsFactoryAndReturnKind(t *testing.T) {
	root := t.TempDir()
	source := `package p;import "errors";func Calc(x int)(int,error){if x<0{return 0,errors.New("bad")};return x+1,nil}`
	if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	allocations, rejections := 0, 0
	for _, artifact := range program.Depth.Flows {
		for _, function := range artifact.Functions {
			for _, block := range function.Blocks {
				for _, in := range block.Instructions {
					if in.Opcode == facts.OpAllocate {
						allocations++
						if len(in.Roots) != 1 || in.Roots[0].Ownership != "owned" || len(in.Provenance) != 1 || in.Provenance[0].RuleID != "go.error" {
							t.Fatalf("factory provenance lost: %+v", in)
						}
					}
					if in.CompletionKind == facts.EdgeReturnError {
						rejections++
						if in.Opcode != facts.OpReturn {
							t.Fatal("error return became panic")
						}
					}
				}
			}
		}
	}
	if allocations != 1 || rejections != 1 {
		t.Fatalf("missing contract flow: %d %d", allocations, rejections)
	}
}
