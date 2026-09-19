package sourceestimate

import (
	"os"
	"strings"
	"testing"
)

func TestNoFindingFromSimpleOrUnknownBehaviorAcrossLanguages(t *testing.T) {
	for _, c := range []struct{ language, path, source string }{
		{"go", "simple.go", `package p;func Identity(x int)int{return x};func predicate(x int)bool{return x>0};func opaque(x int)int{return missing(x)}`},
		{"java", "Simple.java", `class Simple { int identity(int x){return x;} boolean predicate(int x){return x>0;} int opaque(int x){return Missing.apply(x);} }`},
		{"typescript", "simple.ts", `export function identity(x:number){return x;} export function predicate(x:number){return x>0;} export function opaque(x:number){return missing(x);}`},
		{"rust", "simple.rs", `pub fn identity(x:i32)->i32{x} pub fn predicate(x:i32)->bool{x>0} pub fn opaque(x:i32)->i32{missing(x)}`},
	} {
		t.Run(c.language, func(t *testing.T) {
			_, r := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.source)}})
			if r[c.path].PublishedPenalty() > 25 || len(r[c.path].Findings) != 0 {
				t.Fatalf("ordinary small abstraction left the low range or acquired a defect finding: %+v", r[c.path])
			}
		})
	}
}

func TestProcessGroupAliveRetainsCallUncertainty(t *testing.T) {
	data, err := os.ReadFile("../isolation/process_unix.go")
	if err != nil {
		t.Fatal(err)
	}
	_, r := AnalyzeWithAttribution([]File{{Path: "process_unix.go", Language: "go", Source: data}})
	found := false
	for _, a := range r["process_unix.go"].Abstractions {
		if a.Name == "processGroupAlive" {
			for _, reason := range a.Limitations {
				if strings.Contains(reason, "syscall.Kill") {
					found = true
				}
			}
			if len(a.Findings) != 0 {
				t.Fatalf("platform predicate became a finding: %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("assignment erased processGroupAlive call uncertainty: %+v", r)
	}
}

func TestAssignedAndNestedCallsRetainUncertainty(t *testing.T) {
	for _, body := range []string{`x:=missing(); _=x;return true`, `return ignore(missing())`} {
		_, r := AnalyzeWithAttribution([]File{{Path: "calls.go", Language: "go", Source: []byte(`package p;func Run()bool{` + body + `};func ignore(x int)bool{return true}`)}})
		found := false
		for _, abstraction := range r["calls.go"].Abstractions {
			if abstraction.Name != "Run" {
				continue
			}
			for _, reason := range abstraction.Limitations {
				found = found || strings.Contains(reason, "missing")
			}
		}
		if !found || r["calls.go"].PublishedPenalty() != 0 {
			t.Fatalf("lost uncertainty: %+v", r)
		}
	}
}
