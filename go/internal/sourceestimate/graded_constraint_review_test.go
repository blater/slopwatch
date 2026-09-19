package sourceestimate

import (
	"strings"
	"testing"
)

type constraintReviewFixture struct{ lang, path, decl, method, end, receiver, size, closure, caller, irrelevant string }

func constraintReviewFixtures() []constraintReviewFixture {
	return []constraintReviewFixture{
		{"java", "Buffer.java", `public class Buffer{public int[] Data=new int[0];public int Count;`, `public int read(){`, `}}`, `this.`, `this.Data.length`, `Runnable ignored=()->{this.Data[this.Count-1]=1;};return 0;`, `class Caller{int read(Buffer b){return b.Data[b.Count-1];}}`, `public int unrelated(){return 1;}`},
		{"typescript", "Buffer.ts", `export class Buffer{public Data:number[]=[];public Count=0;`, `read():number{`, `}}`, `this.`, `this.Data.length`, `const ignored=()=>{this.Data[this.Count-1]=1;};return 0;`, `import {Buffer} from './Buffer';export function read(b:Buffer){return b.Data[b.Count-1];}`, `unrelated(){return 1;}`},
		{"go", "buffer.go", `package p;type Buffer struct{Data []int;Count int};`, `func(b *Buffer)Read()int{`, `}`, `b.`, `len(b.Data)`, `ignored:=func(){b.Data[b.Count-1]=1};_=ignored;return 0`, `package p;func Read(b *Buffer)int{return b.Data[b.Count-1]}`, `func(b *Buffer)Unrelated()int{return 1}`},
		{"rust", "buffer.rs", `pub struct Buffer{pub Data:Vec<i32>,pub Count:usize}impl Buffer{`, `pub fn read(&mut self)->i32{`, `}}`, `self.`, `self.Data.len()`, `let ignored=||{self.Data[self.Count-1]=1;};return 0;`, `use crate::Buffer;pub fn read(b:&Buffer)->i32{b.Data[b.Count-1]}`, `pub fn unrelated(&self)->i32{1}`},
	}
}
func TestConstraintReviewedIndexAndDeferredMatrix(t *testing.T) {
	for _, tc := range constraintReviewFixtures() {
		t.Run(tc.lang, func(t *testing.T) {
			run := func(body string) *GradedEvidence {
				return structuralResult(tc.lang, tc.path, tc.decl+tc.method+body+tc.end).Grade
			}
			index := tc.receiver + "Data[" + tc.receiver + "Count-1]"
			if g := run(index + "=7;return 0;"); g.Surface.RepresentationUnits != 4 {
				t.Fatalf("indexed write lost bounds: %+v", g)
			}
			if g := run("if(" + tc.size + "==0){return 0;}return " + tc.receiver + "Data[0];"); g.Surface.RepresentationUnits != 0 {
				t.Fatalf("guarded constant access: %+v", g)
			}
			reject := "throw new IllegalStateException();"
			if tc.lang == "typescript" {
				reject = "throw new Error();"
			}
			if tc.lang == "go" {
				reject = `panic("invalid")`
			}
			if tc.lang == "rust" {
				reject = `assert!(false);`
			}
			if g := run("if(" + tc.receiver + "Count!=" + tc.size + "){" + reject + "}return 0;"); g.Surface.RepresentationUnits != 4 {
				t.Fatalf("size equality lost actual constraint: %+v", g)
			}
			if g := run(tc.closure); g.Surface.RepresentationUnits != 0 {
				t.Fatalf("deferred closure invented obligation: %+v", g)
			}
		})
	}
}
func TestConstraintDataOnlyRegistry(t *testing.T) {
	for _, tc := range constraintReviewFixtures() {
		t.Run(tc.lang, func(t *testing.T) {
			base := tc.decl
			if tc.lang == "java" || tc.lang == "typescript" {
				base += "}"
			}
			if tc.lang == "rust" {
				base = strings.TrimSuffix(base, "impl Buffer{")
			}
			withMethod := base
			if tc.lang == "java" || tc.lang == "typescript" {
				withMethod = strings.TrimSuffix(base, "}") + tc.irrelevant + "}"
			} else if tc.lang == "rust" {
				withMethod += "impl Buffer{" + tc.irrelevant + "}"
			} else {
				withMethod += tc.irrelevant
			}
			for _, source := range []string{base, withMethod} {
				files := []File{{Path: tc.path, Language: tc.lang, Source: []byte(source)}, {Path: "caller." + strings.Split(tc.path, ".")[1], Language: tc.lang, Source: []byte(tc.caller)}}
				g := Analyze(files)[tc.path].Grade
				if g == nil || g.Surface.RepresentationUnits != 4 {
					t.Fatalf("method-independent storage: %+v source=%s", g, source)
				}
			}
		})
	}
}

func TestConstraintReviewedConsumerEqualityMatrix(t *testing.T) {
	for _, tc := range []struct{ lang, path, storage, caller string }{
		{"java", "State.java", `public class State{public int Left;public int Right;}`, `class Calls{void set(State s,int x,int y){s.Left=x;s.Right=y;}int read(State s){return check(s.Left,s.Right);}int check(int a,int b){if(a!=b)return -1;return effect(a);}int effect(int a){return a+1;}}`},
		{"typescript", "State.ts", `export class State{public Left=0;public Right=0;}`, `import {State} from './State';export function read(s:State){return check(s.Left,s.Right);}function check(a:number,b:number){if(a!=b)return -1;return effect(a);}function effect(a:number){return a+1;}`},
		{"go", "state.go", `package p;type State struct{Left int;Right int}`, `package p;func Read(s *State)int{return check(s.Left,s.Right)};func check(a,b int)int{if a!=b{return -1};return effect(a)};func effect(a int)int{return a+1}`},
		{"rust", "state.rs", `pub struct State{pub Left:i32,pub Right:i32}`, `use crate::State;pub fn read(s:&State)->i32{return check(s.Left,s.Right);}fn check(a:i32,b:i32)->i32{if a!=b{return -1;}return effect(a);}fn effect(a:i32)->i32{a+1}`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			files := []File{{Path: tc.path, Language: tc.lang, Source: []byte(tc.storage)}, {Path: "caller." + strings.Split(tc.path, ".")[1], Language: tc.lang, Source: []byte(tc.caller)}}
			g := Analyze(files)[tc.path].Grade
			if g == nil || g.Surface.RepresentationUnits != 4 {
				t.Fatalf("resolved equality precondition missing: %+v", g)
			}
			files[1].Source = []byte(strings.ReplaceAll(tc.caller, "a!=b", "a==b"))
			if g := Analyze(files)[tc.path].Grade; g.Surface.RepresentationUnits != 4 {
				t.Fatalf("inverse relational precondition missing: %+v", g)
			}
		})
	}
}

func TestConstraintReviewedOwnerIsolation(t *testing.T) {
	u := gradedSurfaceUnit("java", "Both.java", `class A{int read(){return 1;}}class B{int[] Data;int Count;}`)
	u.callerObligations = map[string]gradedCallerObligation{"B": {fields: []string{"Data", "Count"}, constraints: []gradedConstraint{{id: "B#bounds#Count,Data", kind: "index-bounds", fields: []string{"Count", "Data"}}}}}
	surface := gradedCallerSurface(u, u.ops)
	if surface.RepresentationUnits != 0 || len(surface.Constraints) != 0 {
		t.Fatalf("B burden projected onto A: %+v", surface)
	}
	surface = gradedCallerSurface(u, nil)
	if surface.RepresentationUnits != 4 || len(surface.Constraints) != 1 {
		t.Fatalf("data-owner constraint unavailable: %+v", surface)
	}
}

func TestConstraintReviewedMaintenanceExitAndOffset(t *testing.T) {
	for _, index := range []string{"b.Count-1", "b.Count"} {
		for _, tail := range []string{"", "b.Count=1000;", "b.Data=nil;", "b=&Buffer{};", "mutate(b);"} {
			source := `package p;type Buffer struct{Data []int;Count int};func(b *Buffer)Read()int{return b.Data[` + index + `]};func(b *Buffer)Replace(input []int){b.Data=input;b.Count=len(b.Data);` + tail + `}`
			u := gradedSurfaceUnit("go", "x.go", source)
			for _, op := range u.ops {
				if op.name != "Replace" {
					continue
				}
				want := index == "b.Count-1" && tail == ""
				if gradeConsistentWrites(op, u, op.body) != want {
					t.Fatalf("maintenance offset=%s tail=%s want=%v", index, tail, want)
				}
			}
		}
	}
	for _, reverse := range []bool{false, true} {
		value := `func(b Buffer)Replace(input []int){b.Data=input;b.Count=len(b.Data);b.Count++;}`
		pointer := `func(b *Other)Replace(input []int){b.Data=input;b.Count=len(b.Data);}`
		source := `package p;type Buffer struct{Data []int;Count int};type Other struct{Data []int;Count int};`
		if reverse {
			source += pointer + ";" + value
		} else {
			source += value + ";" + pointer
		}
		u := gradedSurfaceUnit("go", "x.go", source)
		for _, op := range u.ops {
			if gradedMutableGoReceiver(op, u) != (op.owner == "Other") {
				t.Fatalf("receiver identity lost: owner=%s", op.owner)
			}
		}
	}
	g := structuralResult("go", "x.go", `package p;type Buffer struct{Data []int;Count int};func(b Buffer)Replace(input []int){b.Data=input;b.Count=len(b.Data);b.Count++;}`).Grade
	for _, category := range []string{"state", "state-consistency", "invariant-transformation", "transform"} {
		if g.Responsibilities[category] != 0 {
			t.Fatalf("copy-only command credited %s: %+v", category, g)
		}
	}
	guarded := `package p;type Buffer struct{Data []int;Count int};func(b *Buffer)Read()int{if b.Count<=0 || b.Count>len(b.Data){return 0};b=&Buffer{};return b.Data[b.Count-1]}`
	if g := structuralResult("go", "x.go", guarded).Grade; g.Surface.RepresentationUnits != 4 {
		t.Fatalf("replacement retained guard: %+v", g)
	}
}

func TestConstraintReviewedLexicalBuiltinShadow(t *testing.T) {
	base := `package p;type Buffer struct{Data []int;Count int};func(b *Buffer)Read()int{if b.Count<=0 || b.Count>len(b.Data){return 0};return b.Data[b.Count-1]};`
	local := `func unrelated(){var len=1;_ = len}`
	for _, extra := range []string{local, strings.ReplaceAll(local, "len", "size")} {
		g := structuralResult("go", "x.go", base+extra).Grade
		if g.Surface.RepresentationUnits != 0 {
			t.Fatalf("unrelated local shadow leaked: %+v", g)
		}
	}
	files := []File{{Path: "x.go", Language: "go", Source: []byte(base)}, {Path: "shadow.go", Language: "go", Source: []byte(`package p;var len=func(x []int)int{return 1000}`)}}
	if g := Analyze(files)["x.go"].Grade; g.Surface.RepresentationUnits != 4 {
		t.Fatalf("package shadow ignored: %+v", g)
	}
}

func TestConstraintReviewedVariantOutputConnection(t *testing.T) {
	for _, tc := range []struct{ lang, path, prefix, suffix, connected, disconnected, extracted string }{
		{"java", "Example.java", `public class Example{public int read(int x){`, `}private int helper(int x){return x*2;}}`, `switch(x){case 0:return x*2;default:return 0;}`, `switch(x){case 0:helper(x);break;default:break;}return helper(x);`, `switch(x){case 0:return helper(x);default:return 0;}`},
		{"typescript", "example.ts", `export class Example{read(x:number){`, `}private helper(x:number){return x*2;}}`, `switch(x){case 0:return x*2;default:return 0;}`, `switch(x){case 0:this.helper(x);break;default:break;}return this.helper(x);`, `switch(x){case 0:return this.helper(x);default:return 0;}`},
		{"go", "example.go", `package p;type Example struct{};func(e Example)Read(x int)int{`, `};func(e Example)helper(x int)int{return x*2}`, `switch x{case 0:return x*2;default:return 0}`, `switch x{case 0:e.helper(x);default:};return e.helper(x)`, `switch x{case 0:return e.helper(x);default:return 0}`},
		{"rust", "example.rs", `pub struct Example{}impl Example{pub fn read(&self,x:i32)->i32{`, `}fn helper(&self,x:i32)->i32{x*2}}`, `match x{0=>x*2,_=>0}`, `match x{0=>{self.helper(x);},_=>{}};self.helper(x)`, `match x{0=>self.helper(x),_=>0}`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			for _, entry := range []struct {
				body string
				want float64
			}{{tc.connected, 2}, {tc.extracted, 2}, {tc.disconnected, 0}} {
				g := structuralResult(tc.lang, tc.path, tc.prefix+entry.body+tc.suffix).Grade
				if g == nil || g.Responsibilities["representation-transformation"] != entry.want {
					t.Fatalf("branch output %s: %+v", entry.body, g)
				}
			}
		})
	}
}

func TestConstraintReviewedAggregateAllowanceMatrix(t *testing.T) {
	for _, tc := range []struct{ lang, path, prefix, suffix, constructed, copied, declare, overwrite, known string }{
		{"java", "Example.java", `public class Example{private int total;public Object read(int x,int a,int b,int c){`, `}}`, `new Object[]{External.outer(External.inner(x))}`, `new Object[]{x}`, `Object value=`, `value=`, `this.total=x*2;`},
		{"typescript", "example.ts", `export class Example{private total=0;read(x:number,a:number,b:number,c:number){`, `}}`, `{value:external.outer(external.inner(x))}`, `{value:x}`, `let value=`, `value=`, `this.total=x*2;`},
		{"go", "example.go", `package p;type Example struct{total int};func(e *Example)Read(x,a,b,c int)map[string]int{`, `}`, `map[string]int{"value":external.Outer(external.Inner(x))}`, `map[string]int{"value":x}`, `value:=`, `value=`, `e.total=x*2;`},
		{"rust", "example.rs", `pub struct Output{value:i32}pub struct Example{total:i32}impl Example{pub fn read(&mut self,x:i32,a:i32,b:i32,c:i32)->Output{`, `}}`, `Output{value:external::outer(external::inner(x))}`, `Output{value:x}`, `let mut value=`, `value=`, `self.total=x*2;`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			run := func(body string) *GradedEvidence {
				return structuralResult(tc.lang, tc.path, tc.prefix+body+tc.suffix).Grade
			}
			positive := run("return " + tc.constructed + ";")
			if positive == nil || positive.EstimatedHidden != DefaultCalibration().TransformationEnvelope || positive.Responsibilities["validation"] != 0 || positive.Hidden != 0 || positive.Value() != 13 {
				t.Fatalf("aggregate inferred non-transform duty: %+v", positive)
			}
			for _, body := range []string{"return " + tc.copied + ";", tc.declare + tc.constructed + ";return " + tc.copied + ";", tc.declare + tc.constructed + ";" + tc.overwrite + tc.copied + ";return value;"} {
				g := run(body)
				if g.EstimatedHidden != 0 || g.Hidden != 0 || (g.Value() < 0 || g.Value() > 100) {
					t.Fatalf("copy/discard/overwrite gained responsibility: %s %+v", body, g)
				}
			}
			known := run(tc.known + "return " + tc.constructed + ";")
			if known.Responsibilities["invariant-transformation"] == 0 || known.EstimatedHidden != 0 || known.Responsibilities["validation"] != 0 {
				t.Fatalf("known transform was not deducted: %+v", known)
			}
		})
	}
}

func TestConstraintReviewedUnrelatedVariantSinks(t *testing.T) {
	for _, tc := range []struct{ lang, path, source string }{
		{"java", "Example.java", `class Example{int[] value;void run(int x,java.util.List<Integer> output){switch(x){case 0:break;default:break;}output.add(helper(x));this.value=new int[]{helper(x)};}int helper(int x){return x*2;}}`},
		{"typescript", "example.ts", `class Example{value:any;run(x:number,output:number[]){switch(x){case 0:break;default:break;}output.push(this.helper(x));this.value={value:this.helper(x)};}private helper(x:number){return x*2;}}`},
		{"go", "example.go", `package p;type Output interface{Add(int)};type Example struct{Value map[string]int};func(e *Example)Run(x int,output Output){switch x{case 0:default:};output.Add(helper(x));e.Value=map[string]int{"value":helper(x)}};func helper(x int)int{return x*2}`},
		{"rust", "example.rs", `struct Value{value:i32}struct Example{value:Value}impl Example{fn run(&mut self,x:i32,output:&mut Vec<i32>){match x{0=>{},_=>{}};output.push(helper(x));self.value=Value{value:helper(x)};}}fn helper(x:i32)->i32{x*2}`},
	} {
		u := gradedSurfaceUnit(tc.lang, tc.path, tc.source)
		for _, op := range u.ops {
			if !strings.EqualFold(op.name, "run") {
				continue
			}
			if op.owner == "" {
				op.owner = "Example"
			}
			op.outputBuffers = []string{"output"}
			if gradeTypedAlternatives(op, u, op.body) {
				t.Fatalf("%s unrelated output sink acquired variant responsibility", tc.lang)
			}
		}
	}
}

func TestConstraintGoParallelWritesInvalidateProof(t *testing.T) {
	for _, tail := range []string{"b.Count, other=1000,0;", "other,b.Count=0,1000;", "b,other=replacement,0;", "other,b=0,replacement;", "b.Data,other=nil,0;", "other,b.Data=0,nil;", "other,another=0,1;"} {
		t.Run(tail, func(t *testing.T) {
			harmless := tail == "other,another=0,1;"
			prefix := `package p;type Buffer struct{Data []int;Count int};`
			guarded := prefix + `func(b *Buffer)Read(other,another int,replacement *Buffer)int{if b.Count<=0 || b.Count>len(b.Data){return 0};` + tail + `return b.Data[b.Count-1]}`
			g := structuralResult("go", "x.go", guarded).Grade
			want := float64(4)
			if harmless {
				want = 0
			}
			if g.Surface.RepresentationUnits != want {
				t.Fatalf("parallel guard invalidation: %+v", g)
			}
			source := prefix + `func(b *Buffer)Read()int{return b.Data[b.Count-1]};func(b *Buffer)Replace(input []int,other,another int,replacement *Buffer){b.Data=input;b.Count=len(b.Data);` + tail + `}`
			u := gradedSurfaceUnit("go", "x.go", source)
			for _, op := range u.ops {
				if op.name == "Replace" && gradeConsistentWrites(op, u, op.body) != harmless {
					t.Fatalf("parallel maintenance invalidation failed: %s", tail)
				}
			}
		})
	}
}

func TestConstraintRustParameterizedMoveClosuresAreDeferred(t *testing.T) {
	for _, parameter := range []string{"x", "x:i32"} {
		t.Run(parameter, func(t *testing.T) {
			owner := `pub struct Buffer{pub Data:Vec<i32>,pub Count:usize}impl Buffer{pub fn read(&mut self)->i32{let ignored=move |` + parameter + `|{let _:i32=x;self.Data[self.Count-1]};return 0;}}`
			g := structuralResult("rust", "x.rs", owner).Grade
			if g.Surface.RepresentationUnits != 0 {
				t.Fatalf("deferred owner constraint: %+v", g)
			}
			output := `pub fn collect(output:&mut Vec<i32>){let ignored=move |` + parameter + `|{output.push(x);};}`
			g = structuralResult("rust", "x.rs", output).Grade
			if g.Surface.RepresentationUnits != 0 {
				t.Fatalf("deferred output obligation: %+v", g)
			}
			files := []File{{Path: "state.rs", Language: "rust", Source: []byte(`pub struct State{pub active:bool,pub value:i32}`)}, {Path: "calls.rs", Language: "rust", Source: []byte(`use crate::State;pub fn open(s:&mut State){let ignored=move |` + parameter + `|{let _:i32=x;s.active=true;};}pub fn close(s:&mut State){s.active=false;}pub fn next(s:&mut State)->i32{if !s.active{return -1;}s.value+=1;return s.value;}`)}}
			g = Analyze(files)["state.rs"].Grade
			if g != nil && g.Surface.RepresentationUnits != 0 {
				t.Fatalf("deferred phase producer: %+v", g)
			}
		})
	}
}
