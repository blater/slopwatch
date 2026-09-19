package sourceestimate

import (
	"strings"
	"testing"
)

func TestConstraintCouplingMatrix(t *testing.T) {
	for _, tc := range []struct{ lang, path, prefix, suffix, projection, index, guard, extra, shadow string }{
		{"java", "Example.java", `public class Example{public int[] data;public int count;public int metadata; public int read(){`, `}}`, `return count*metadata;`, `return data[count-1];`, `if(count<=0 || count>data.length){return 0;}`, `int unused=metadata+1;`, `int count=1;int[] data=new int[1];return data[count-1];`},
		{"typescript", "example.ts", `export class Example{public data:number[]=[];public count:number=0;public metadata:number=0; read():number{`, `}}`, `return this.count*this.metadata;`, `return this.data[this.count-1];`, `if(this.count<=0 || this.count>this.data.length){return 0;}`, `const unused=this.metadata+1;`, `const count=1;const data=[1];return data[count-1];`},
		{"go", "example.go", `package p;type Example struct{Data []int;Count int;Metadata int};func(e Example)Read()int{`, `}`, `return e.Count*e.Metadata`, `return e.Data[e.Count-1]`, `if e.Count<=0 || e.Count>len(e.Data){return 0};`, `unused:=e.Metadata+1;_ = unused;`, `count:=1;data:=[]int{1};return data[count-1]`},
		{"rust", "example.rs", `pub struct Example{pub data:Vec<i32>,pub count:usize,pub metadata:i32}impl Example{pub fn read(&self)->i32{`, `}}`, `return (self.count as i32)*self.metadata;`, `return self.data[self.count-1];`, `if self.count<=0 || self.count>self.data.len(){return 0;}`, `let unused=self.metadata+1;`, `let count=1;let data=vec![1];return data[count-1];`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			grade := func(body string) *GradedEvidence {
				return structuralResult(tc.lang, tc.path, tc.prefix+body+tc.suffix).Grade
			}
			for name, body := range map[string]string{"arithmetic": tc.projection, "shadow": tc.shadow} {
				if g := grade(body); g == nil || g.Surface.RepresentationUnits != 0 {
					t.Fatalf("%s invented constraint: %+v", name, g)
				}
			}
			base, extra, guard := grade(tc.index), grade(tc.extra+tc.index), grade(tc.guard+tc.index)
			if base.Surface.RepresentationUnits != 4 || extra.Surface.RepresentationUnits != base.Surface.RepresentationUnits {
				t.Fatalf("exact participants: base=%+v extra=%+v", base, extra)
			}
			if guard.Surface.RepresentationUnits != 0 || guard.Value() > base.Value() {
				t.Fatalf("bounds protection: base=%+v guard=%+v", base, guard)
			}
			evidence := strings.Join(base.Surface.Evidence, "\n")
			if !strings.Contains(evidence, "constraint:index-bounds") || !strings.Contains(evidence, "location="+tc.path+":1:") {
				t.Fatalf("missing located constraint: %s", evidence)
			}
		})
	}
}

func TestConstraintProjectionAndMapLookupAreNotBounds(t *testing.T) {
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `public class Example{public int a,b;public int read(){if(a==b){return a;}return b;}}`},
		{"typescript", "example.ts", `export class Example{public a:number;public b:number;read(){return {a:this.a,b:this.b};}}`},
		{"go", "example.go", `package p;type Example struct{Data map[int]int;Key int};func(e Example)Read()int{return e.Data[e.Key]}`},
		{"go", "write.go", `package p;type Example struct{Data map[int]int;Key int};func(e Example)Write(){e.Data[e.Key]=7}`},
		{"rust", "example.rs", `pub struct Example{pub a:i32,pub b:i32}impl Example{pub fn read(&self)->i32{if self.a==self.b{return self.a;}return self.b;}}`},
	} {
		if g := structuralResult(tc.language, tc.path, tc.source).Grade; g.Surface.RepresentationUnits != 0 {
			t.Fatalf("%s invented obligation: %+v", tc.language, g)
		}
	}
}

func TestConstraintGuardMustDominateAndMatchStorage(t *testing.T) {
	prefix := `package p;type Example struct{Data []int;Count int;Other int};func(e Example)Read()int{`
	for _, guard := range []string{`if e.Other>0{if e.Count<=0 || e.Count>len(e.Data){return 0}};`, `if e.Other<=0 || e.Other>len(e.Data){return 0};`, `if e.Count<=0 || e.Count>len(e.Data){return 0};e.Count=100;`} {
		g := structuralResult("go", "x.go", prefix+guard+`return e.Data[e.Count-1]}`).Grade
		if g.Surface.RepresentationUnits < 4 {
			t.Fatalf("unproven protection: %+v", g)
		}
	}
}

func TestConstraintCallerDoesNotBorrowOwnFields(t *testing.T) {
	files := []File{{Path: "Target.java", Language: "java", Source: []byte(`package p;class Target{int[] data;int count;void reset(){count=0;}}`)}, {Path: "Caller.java", Language: "java", Source: []byte(`package p;class Caller{int[] data;int count;int read(Target target){int ignored=target.count;int[] other=target.data;return this.data[this.count-1];}}`)}}
	if g := Analyze(files)["Target.java"].Grade; g.Surface.RepresentationUnits != 0 {
		t.Fatalf("caller-owned storage became target constraint: %+v", g)
	}
	files[1].Source = []byte(`package p;class Caller{int read(Target target){return target.data[target.count-1];}}`)
	if g := Analyze(files)["Target.java"].Grade; g.Surface.RepresentationUnits != 4 {
		t.Fatalf("actual caller constraint missing: %+v", g)
	}
}

func TestConstraintConsumersRequireActualPreconditions(t *testing.T) {
	state := File{Path: "State.java", Language: "java", Source: []byte(`package p;class State{int lower;int upper;boolean active;void reset(){lower=0;upper=0;}}`)}
	producer := File{Path: "Producer.java", Language: "java", Source: []byte(`package p;class Producer{void open(State s){s.lower=1;s.upper=3;s.active=true;}void close(State s){s.active=false;}}`)}
	driver := File{Path: "Driver.java", Language: "java", Source: []byte(`package p;class Driver{int range(int a,int b){if(b<=a)return -1;return effect(a,b);}int effect(int a,int b){return a+b;}}`)}
	for _, tc := range []struct {
		body string
		want float64
	}{
		{`boolean pick=s.active?true:false;return d.effect(s.lower,s.upper);`, 0},
		{`if(s.active){return d.effect(s.lower,s.upper);}return 0;`, 0},
		{`return d.range(s.lower,s.upper);`, 4},
		{`if(!s.active)return -1;return d.effect(s.lower,s.upper);`, 4},
	} {
		caller := File{Path: "Caller.java", Language: "java", Source: []byte(`package p;class Caller{Driver d;int read(State s){` + tc.body + `}}`)}
		g := Analyze([]File{state, producer, driver, caller})["State.java"].Grade
		if g.Surface.RepresentationUnits != tc.want {
			t.Fatalf("consumer %s: %+v", tc.body, g)
		}
	}
}

func TestConstraintAggregationRenameAndHelpers(t *testing.T) {
	for _, tc := range []struct{ lang, path, source, aggregate string }{
		{"java", "Example.java", `public class Example{public int[] data;public int count;public int meta;public int read(){return last();}private int last(){return data[count-1];}}`, `public class Example{public int width;public int height;public int metadata;public Object read(){return java.util.Map.of("width",width,"height",height,"area",width*height,"metadata",metadata);}}`},
		{"typescript", "example.ts", `export class Example{public data:number[]=[];public count=0;public meta=0;read(){return this.last();}private last(){return this.data[this.count-1];}}`, `export class Example{public width=0;public height=0;public metadata=0;read(){return {width:this.width,height:this.height,area:this.width*this.height,metadata:this.metadata};}}`},
		{"go", "example.go", `package p;type Example struct{Data []int;Count int;Meta int};func(e Example)Read()int{return e.last()};func(e Example)last()int{return e.Data[e.Count-1]}`, `package p;type Example struct{Width int;Height int;Metadata int};func(e Example)Read()map[string]int{return map[string]int{"width":e.Width,"height":e.Height,"area":e.Width*e.Height,"metadata":e.Metadata}}`},
		{"rust", "example.rs", `pub struct Example{pub data:Vec<i32>,pub count:usize,pub meta:i32}impl Example{pub fn read(&self)->i32{self.last()}fn last(&self)->i32{self.data[self.count-1]}}`, `pub struct Example{pub width:i32,pub height:i32,pub metadata:i32}impl Example{pub fn read(&self)->(i32,i32,i32,i32){(self.width,self.height,self.width*self.height,self.metadata)}}`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			g := structuralResult(tc.lang, tc.path, tc.source).Grade
			renamed := strings.NewReplacer("data", "items", "count", "used", "Data", "Items", "Count", "Used", "last", "tail").Replace(tc.source)
			r := structuralResult(tc.lang, tc.path, renamed).Grade
			if g.Surface.RepresentationUnits != 4 || r.Surface.RepresentationUnits != 4 {
				t.Fatalf("helper/rename invariance: %+v / %+v", g, r)
			}
			a := structuralResult(tc.lang, tc.path, tc.aggregate).Grade
			if a.Surface.RepresentationUnits != 0 {
				t.Fatalf("aggregate invented obligation: %+v", a)
			}
		})
	}
}

func TestConstraintPhasePolarityAcrossLanguages(t *testing.T) {
	for _, tc := range []struct{ lang, path, target, caller, field string }{
		{"java", "State.java", `package p;class State{boolean active;void reset(){active=false;}}`, `package p;class Calls{void open(State s){s.active=true;}void close(State s){s.active=false;}int next(State s){if(CONDITION)return -1;return effect();}int effect(){return 1;}}`, "s.active"},
		{"typescript", "State.ts", `export class State{public active=false;reset(){this.active=false;}}`, `import {State} from './State';export function open(s:State){s.active=true;}export function close(s:State){s.active=false;}export function next(s:State){if(CONDITION)return -1;return effect();}function effect(){return 1;}`, "s.active"},
		{"go", "state.go", `package p;type State struct{Active bool};func(s *State)Reset(){s.Active=false}`, `package p;func Open(s *State){s.Active=true};func Close(s *State){s.Active=false};func Next(s *State)int{if CONDITION{return -1};return effect()};func effect()int{return 1}`, "s.Active"},
		{"rust", "state.rs", `pub struct State{pub active:bool}impl State{pub fn reset(&mut self){self.active=false;}}`, `use crate::State;pub fn open(s:&mut State){s.active=true;}pub fn close(s:&mut State){s.active=false;}pub fn next(s:&State)->i32{if CONDITION{return -1;}return effect();}fn effect()->i32{1}`, "s.active"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			for _, condition := range []string{"!" + tc.field, tc.field + "==false", tc.field + "!=true", tc.field, tc.field + "==true"} {
				files := []File{{Path: tc.path, Language: tc.lang, Source: []byte(tc.target)}, {Path: "caller." + strings.Split(tc.path, ".")[1], Language: tc.lang, Source: []byte(strings.ReplaceAll(tc.caller, "CONDITION", condition))}}
				g := Analyze(files)[tc.path].Grade
				if g == nil || g.Surface.RepresentationUnits != 4 {
					t.Fatalf("phase %s: %+v", condition, g)
				}
			}
			closure := "Runnable ignored=()->{" + tc.field + "=true;}"
			switch tc.lang {
			case "typescript":
				closure = "const ignored=()=>{" + tc.field + "=true;}"
			case "go":
				closure = "ignored:=func(){" + tc.field + "=true};_=ignored"
			case "rust":
				closure = "let ignored=||{" + tc.field + "=true;}"
			}
			base := strings.ReplaceAll(tc.caller, "CONDITION", "!"+tc.field)
			for _, caller := range []string{
				strings.Replace(base, tc.field+"=true", tc.field+"=true&&false", 1),
				strings.Replace(base, tc.field+"=false", tc.field+"=false||true", 1),
				strings.Replace(base, tc.field+"=true", "if(false){"+tc.field+"=true;}", 1),
				strings.Replace(base, tc.field+"=true", closure, 1),
			} {
				files := []File{{Path: tc.path, Language: tc.lang, Source: []byte(tc.target)}, {Path: "caller." + strings.Split(tc.path, ".")[1], Language: tc.lang, Source: []byte(caller)}}
				if g := Analyze(files)[tc.path].Grade; g.Surface.RepresentationUnits != 0 {
					t.Fatalf("unconfirmed/deferred phase: %+v caller=%s", g, caller)
				}
			}

		})
	}
}

func TestConstraintMaintenanceRequiresMatchingDerivation(t *testing.T) {
	for _, tc := range []struct{ lang, path, source, valid, invalid string }{
		{"java", "Example.java", `public class Example{public int[] data;public int count;public int last(){return data[count-1];}public void replace(int[] input){BODY}}`, `data=input;count=data.length;`, `data=input;count=1000;`},
		{"typescript", "example.ts", `export class Example{public data:number[]=[];public count=0;last(){return this.data[this.count-1];}replace(input:number[]){BODY}}`, `this.data=input;this.count=this.data.length;`, `this.data=input;this.count=1000;`},
		{"go", "example.go", `package p;type Example struct{Data []int;Count int};func(e *Example)Last()int{return e.Data[e.Count-1]};func(e *Example)replace(input []int){BODY}`, `e.Data=input;e.Count=len(e.Data);`, `e.Data=input;e.Count=len(input)+1000;`},
		{"rust", "example.rs", `pub struct Example{pub data:Vec<i32>,pub count:usize}impl Example{pub fn last(&self)->i32{self.data[self.count-1]}pub fn replace(&mut self,input:Vec<i32>){BODY}}`, `self.data=input;self.count=self.data.len();`, `self.data=input;self.count=1000;`},
	} {
		for _, entry := range []struct {
			body string
			want bool
		}{{tc.valid, true}, {tc.invalid, false}} {
			source := strings.ReplaceAll(tc.source, "BODY", entry.body)
			u := gradedSurfaceUnit(tc.lang, tc.path, source)
			for _, op := range u.ops {
				if op.owner == "" {
					op.owner = "Example"
				}
				if strings.EqualFold(op.name, "replace") && gradeConsistentWrites(op, u, op.body) != entry.want {
					t.Fatalf("%s coherent derivation=%v source=%s", tc.lang, entry.want, source)
				}
			}
		}
	}
	source := `package p;type Example struct{Data []int;Count int};func(e Example)Last()int{return e.Data[e.Count-1]};func(e Example)replace(input []int){e.Data=input;e.Count=len(e.Data);}`
	u := gradedSurfaceUnit("go", "example.go", source)
	for _, op := range u.ops {
		if op.name == "replace" && gradeConsistentWrites(op, u, op.body) {
			t.Fatal("Go value receiver acquired owner maintenance")
		}
	}
}

func TestConstraintEvidenceSourcePositions(t *testing.T) {
	source := "package p\n// brackets [ ] in comment must not shift evidence\ntype Example struct {\n Data []int\n Count int\n}\nfunc(e Example)Read()int{ return e.Data[e.Count-1] }\n"
	g := structuralResult("go", "example.go", source).Grade
	if len(g.Surface.Constraints) != 1 {
		t.Fatalf("missing constraints: %+v", g)
	}
	record := g.Surface.Constraints[0]
	if record.Line != 7 || record.Offset != strings.Index(source, "[e.Count") || len(record.Storage) != 2 || len(record.CallerControlled) != 2 {
		t.Fatalf("incorrect exact source location/storage: %+v", record)
	}
}

func TestConstraintProtectionAdversaries(t *testing.T) {
	prefix := `package p;type Example struct{Data []int;Count int;Other bool};func(e *Example)Read()int{`
	for _, guard := range []string{
		`if e.Other || (e.Count<=0 && e.Count>len(e.Data)){return 0};`,
		`if e.Count<=0 || e.Count>len(e.Data){if e.Other{return 0}};`,
		`if e.Count<=0 || e.Count>len(e.Data){return 0};e.change();`,
	} {
		g := structuralResult("go", "x.go", prefix+guard+`return e.Data[e.Count-1]}`).Grade
		if g.Surface.RepresentationUnits < 4 {
			t.Fatalf("unsafe protection accepted: %+v", g)
		}
	}
	for _, source := range []string{
		prefix + `if e.Count<=0 || e.Count>len(e.Data){return 0};return e.Data[e.Count-1]};var len=func(x []int)int{return 1000}`,
		`package p;type Example struct{Data []int;Count int};func(e *Example)Read()int{if e.Count<=0 || e.Count>len(e.Data){return 0};return e.Data[e.Count-1]};func len(x []int)int{return 1000}`,
	} {
		g := structuralResult("go", "x.go", source).Grade
		if g.Surface.RepresentationUnits < 4 {
			t.Fatalf("shadowed size contract accepted: %+v", g)
		}
	}
	for _, methods := range []string{
		`func(e Example)Unsafe()int{return e.Data[e.Count-1]};func(e Example)Safe()int{if e.Count<=0 || e.Count>len(e.Data){return 0};return e.Data[e.Count-1]}`,
		`func(e Example)Safe()int{if e.Count<=0 || e.Count>len(e.Data){return 0};return e.Data[e.Count-1]};func(e Example)Unsafe()int{return e.Data[e.Count-1]}`,
	} {
		u := gradedSurfaceUnit("go", "x.go", `package p;type Example struct{Data []int;Count int};`+methods)
		surface := gradedCallerSurface(u, u.ops)
		if len(surface.Constraints) != 1 || surface.Constraints[0].InternallyProtected || !strings.Contains(surface.Constraints[0].Operation, "Unsafe") {
			t.Fatalf("wrong unsafe witness retained: %+v", surface)
		}
	}
}
