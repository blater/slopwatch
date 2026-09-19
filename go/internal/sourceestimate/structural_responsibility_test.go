package sourceestimate

import (
	"strings"
	"testing"
)

func structuralResult(language, path, source string) Result {
	files := []File{{Path: path, Language: language, Source: []byte(source)}}
	results := Analyze(files)
	if language == "rust" {
		results, _ = AnalyzeRustAttribution(files)
	}
	if language == "go" {
		results = AnalyzeGoFiles(files)
	}
	result := results[path]
	for _, abstraction := range result.Abstractions {
		if strings.HasSuffix(abstraction.Name, "Example") && abstraction.Grade != nil {
			result.Grade = abstraction.Grade
			break
		}
	}
	return result
}

func TestStructuralResponsibilityIdentifierDecoys(t *testing.T) {
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `public class Example { public static int run(int x,int a,int b,int c) { int open=x; try { return x+a+b+c; } finally { int close=0; } } }`},
		{"typescript", "example.ts", `export function run(x:number,a:number,b:number,c:number):number {let open=x;try{return x+a+b+c;}finally{let close=0;}}`},
		{"go", "example.go", `package sample;func Run(x,a,b,c int)int{open:=x;close:=0;_ = open;_ = close;defer func(){}();return x+a+b+c}`},
		{"rust", "lib.rs", `pub fn run(x:i32,a:i32,b:i32,c:i32)->i32{let open=x;let close=0;drop(close);x+a+b+c}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			first := structuralResult(tc.language, tc.path, tc.source)
			renamed := strings.NewReplacer("open", "alpha", "close", "omega").Replace(tc.source)
			second := structuralResult(tc.language, tc.path, renamed)
			if first.Grade == nil || second.Grade == nil {
				t.Fatal("missing grades")
			}
			if first.Grade.Value() != second.Grade.Value() || first.Grade.Hidden != second.Grade.Hidden || first.Grade.Responsibilities["resource"] != 0 || second.Grade.Responsibilities["resource"] != 0 {
				t.Fatalf("decoy changes responsibility: %+v / %+v", first.Grade, second.Grade)
			}
		})
	}
}

func TestStructuralStorageNormalization(t *testing.T) {
	for _, tc := range []struct {
		language, path, prefix, suffix string
		bodies                         []string
	}{
		{"java", "Example.java", `public class Example { private int value; public int run(int x,int a,int b,int c) {`, `} }`, []string{`value+=x;return value;`, `this.value+=x;return this.value;`, `value=value+x;return value;`, `this.value=this.value+x;return this.value;`, `Example alias=this;alias.value+=x;return alias.value;`}},
		{"typescript", "example.ts", `export class Example { private value:number=0;run(x:number,a:number,b:number,c:number):number {`, `}}`, []string{`this.value+=x;return this.value;`, `this.value=this.value+x;return this.value;`, `const alias=this;alias.value+=x;return alias.value;`}},
		{"go", "example.go", `package sample;type Example struct{value int};func(e *Example)Run(x,a,b,c int)int{`, `}`, []string{`e.value+=x;return e.value`, `e.value=e.value+x;return e.value`, `alias:=e;alias.value+=x;return alias.value`}},
		{"rust", "lib.rs", `pub struct Example{value:i32}impl Example{pub fn run(&mut self,x:i32,a:i32,b:i32,c:i32)->i32{`, `}}`, []string{`self.value+=x;self.value`, `self.value=self.value+x;self.value`}},
	} {
		t.Run(tc.language, func(t *testing.T) {
			var first *GradedEvidence
			for _, body := range tc.bodies {
				got := structuralResult(tc.language, tc.path, tc.prefix+body+tc.suffix).Grade
				if got == nil {
					t.Fatal("missing grade")
				}
				if first == nil {
					first = got
				}
				if got.Value() != first.Value() || got.Hidden != first.Hidden {
					t.Fatalf("syntax %s: %+v / %+v", body, first, got)
				}
				if got.Responsibilities["state"] != 0 || got.Responsibilities["invariant-transformation"] == 0 {
					t.Fatalf("computed effect not classified once: %+v", got)
				}
			}
		})
	}
}

func TestStructuralConnectedCleanup(t *testing.T) {
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`},
		{"typescript", "example.ts", `class Driver{active=false;begin(){this.active=true;}use(){if(!this.active)throw new Error();return 1;}end(){this.active=false;}}export class Example{private d=new Driver();run(){this.d.begin();try{return this.d.use();}finally{this.d.end();}}}`},
		{"go", "example.go", `package sample;type Driver struct{active bool};func(d *Driver)Begin(){d.active=true};func(d *Driver)Use()int{if !d.active {panic("inactive")};return 1};func(d *Driver)End(){d.active=false};type Example struct{d Driver};func(e *Example)Run()int{e.d.Begin();defer e.d.End();return e.d.Use()}`},
		{"rust", "lib.rs", `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{assert!(self.active);1}fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);let out=guard.0.use_value();drop(guard);out}}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			first := structuralResult(tc.language, tc.path, tc.source).Grade
			renamed := strings.NewReplacer("begin", "alpha", "Begin", "Alpha", "end", "omega", "End", "Omega", "active", "ready").Replace(tc.source)
			second := structuralResult(tc.language, tc.path, renamed).Grade
			if first == nil || second == nil || first.Responsibilities["resource"] != DefaultCalibration().Resource || second.Responsibilities["resource"] != DefaultCalibration().Resource {
				t.Fatalf("connected duty missing: %+v / %+v", first, second)
			}
			if first.Value() != second.Value() || first.Hidden != second.Hidden {
				t.Fatalf("protocol rename changed duty: %+v / %+v", first, second)
			}
			extracted := tc.source
			switch tc.language {
			case "java":
				extracted = strings.ReplaceAll(extracted, "void end(){active=false;}", "void end(){clear();}private void clear(){active=false;}")
			case "typescript":
				extracted = strings.ReplaceAll(extracted, "end(){this.active=false;}", "end(){this.clear();}private clear(){this.active=false;}")
			case "go":
				extracted = strings.ReplaceAll(extracted, "func(d *Driver)End(){d.active=false}", "func(d *Driver)End(){d.clear()};func(d *Driver)clear(){d.active=false}")
			case "rust":
				extracted = strings.ReplaceAll(extracted, "fn end(&mut self){self.active=false;}", "fn end(&mut self){self.clear();}fn clear(&mut self){self.active=false;}")
			}
			conditional := tc.source
			switch tc.language {
			case "java":
				conditional = strings.ReplaceAll(conditional, "finally{d.end();}", "finally{if(d.active)d.end();}")
			case "typescript":
				conditional = strings.ReplaceAll(conditional, "finally{this.d.end();}", "finally{if(this.d.active)this.d.end();}")
			case "go":
				conditional = strings.ReplaceAll(conditional, "defer e.d.End();", "if e.d.active {defer e.d.End()};")
			case "rust":
				conditional = strings.ReplaceAll(conditional, "self.0.end();", "if self.0.active {self.0.end();}")
			}
			negative := structuralResult(tc.language, tc.path, conditional).Grade
			if negative == nil || negative.Responsibilities["resource"] != 0 {
				t.Fatalf("conditional cleanup gained recognized duty: %+v", negative)
			}
			helper := structuralResult(tc.language, tc.path, extracted).Grade
			if helper == nil || helper.Responsibilities["resource"] != first.Responsibilities["resource"] || helper.Hidden != first.Hidden || helper.Value() != first.Value() {
				t.Fatalf("cleanup helper changed duty: %+v / %+v", first, helper)
			}
		})
	}
}

func TestStructuralCleanupNegatives(t *testing.T) {
	prefix := `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}}public class Example{private Driver d=new Driver();private Driver other=new Driver();public int run(boolean condition){`
	for _, body := range []string{
		`d.begin();try{return d.use();}finally{other.end();}`,
		`d.begin();try{return d.use();}finally{if(condition)d.end();}`,
		`d.begin();d=new Driver();try{return d.use();}finally{d.end();}`,
	} {
		got := structuralResult("java", "Example.java", prefix+body+`}}`).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("invented cleanup for %s: %+v", body, got)
		}
	}
	for _, source := range []string{
		`struct Driver{active:bool}impl Driver{fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);struct Other<'a>(&'a mut Driver);impl Drop for Other<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self){let guard=Guard(&mut self.d);drop(guard);}}`,
		`struct Driver{active:bool}impl Driver{fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self){let guard=Guard(&mut self.d);std::mem::forget(guard);}}`,
	} {
		got := structuralResult("rust", "lib.rs", source).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("unrelated or escaped Drop: %+v", got)
		}
	}
}

func TestStructuralStorageIndependentAndShadowed(t *testing.T) {
	source := `public class Example{private int value;private int count;public int run(int x){this.value+=x;this.count++;if(x<0)throw new RuntimeException();return x*2;}}`
	got := structuralResult("java", "Example.java", source).Grade
	for _, category := range []string{"state", "invariant-transformation", "validation"} {
		if got.Responsibilities[category] == 0 {
			t.Fatalf("lost independent %s: %+v", category, got)
		}
	}
	shadow := structuralResult("java", "Example.java", `public class Example{private int value;public int run(int value){value+=2;return value;}}`).Grade
	if shadow.Responsibilities["state"] != 0 || shadow.Responsibilities["invariant-transformation"] != 0 {
		t.Fatalf("parameter became owned field: %+v", shadow)
	}
}

func TestStructuralReviewCleanupPathsAndFinalReset(t *testing.T) {
	driver := `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){RELEASE}}`
	owner := `public class Example{private Driver d=new Driver();public int run(boolean flag){BODY}}`
	for _, tc := range []struct{ name, body, release string }{
		{"conditional protected region", `d.begin();if(flag){try{return d.use();}finally{d.end();}}return d.use();`, `active=false;`},
		{"use before try", `d.begin();int value=d.use();try{return value;}finally{d.end();}`, `active=false;`},
		{"use after finally", `d.begin();try{}finally{d.end();}return d.use();`, `active=false;`},
		{"skippable cleanup loop", `d.begin();try{return d.use();}finally{while(flag){d.end();}}`, `active=false;`},
		{"cleanup false expression", `d.begin();try{return d.use();}finally{d.end();}`, `active=false||true;`},
		{"reset overwritten", `d.begin();try{return d.use();}finally{d.end();}`, `active=false;active=true;`},
		{"reset only else", `d.begin();try{return d.use();}finally{d.end();}`, `if(active){}else{active=false;}`},
		{"exit before protected use", `d.begin();if(flag)return 0;try{return d.use();}finally{d.end();}`, `active=false;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := strings.ReplaceAll(driver, "RELEASE", tc.release) + strings.ReplaceAll(owner, "BODY", tc.body)
			got := structuralResult("java", "Example.java", source).Grade
			if got == nil || got.Responsibilities["resource"] != 0 {
				t.Fatalf("unsupported cleanup recognized: %+v", got)
			}
		})
	}
}

func TestStructuralReviewIncrementSpellingsAndScope(t *testing.T) {
	prefix := `public class Example{private int value;private int count;public void run(int x,boolean flag){`
	for _, body := range []string{`value+=1;count+=1;`, `value+=1;count++;`, `value=value+1;count=count+1;`, `++value;++count;`} {
		got := structuralResult("java", "Example.java", prefix+body+`}}`).Grade
		if got == nil || got.Hidden != 2 || got.Responsibilities["state"] != 2 || got.Responsibilities["invariant-transformation"] != 0 {
			t.Fatalf("unit step syntax %s: %+v", body, got)
		}
	}
	for _, body := range []string{`value+=1;{int value=2;}`, `{int value=2;}value+=1;`} {
		got := structuralResult("java", "Example.java", prefix+body+`}}`).Grade
		if got == nil || got.Responsibilities["state"] != 2 {
			t.Fatalf("nested declaration erased prior/outer field: %s %+v", body, got)
		}
	}
	for _, body := range []string{`Example alias=null;if(flag){alias=this;}alias.value+=x;`, `Example alias=null;other.alias=this;alias.value+=x;`} {
		got := structuralResult("java", "Example.java", prefix+body+`}}`).Grade
		if got == nil || got.Responsibilities["invariant-transformation"] != 0 {
			t.Fatalf("unproved receiver alias became owned: %s %+v", body, got)
		}
	}
}

func TestStructuralReviewGroupedReceivers(t *testing.T) {
	for _, tc := range []struct{ language, path, direct, grouped string }{
		{"java", "Example.java", `public class Example{private int value;public int run(int x){this.value+=x;return this.value;}}`, `public class Example{private int value;public int run(int x){(this).value+=x;return (this).value;}}`},
		{"typescript", "example.ts", `export class Example{private value=0;run(x:number){this.value+=x;return this.value;}}`, `export class Example{private value=0;run(x:number){(this).value+=x;return (this).value;}}`},
		{"go", "example.go", `package sample;type Example struct{value int};func(e *Example)Run(x int)int{e.value+=x;return e.value}`, `package sample;type Example struct{value int};func(e *Example)Run(x int)int{(*e).value+=x;return (*e).value}`},
		{"rust", "lib.rs", `pub struct Example{value:i32}impl Example{pub fn run(&mut self,x:i32)->i32{self.value+=x;self.value}}`, `pub struct Example{value:i32}impl Example{pub fn run(&mut self,x:i32)->i32{(self).value+=x;(self).value}}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			a := structuralResult(tc.language, tc.path, tc.direct).Grade
			b := structuralResult(tc.language, tc.path, tc.grouped).Grade
			if a == nil || b == nil || a.Value() != b.Value() || a.Hidden != b.Hidden || a.Responsibilities["state"] != b.Responsibilities["state"] || a.Responsibilities["invariant-transformation"] != b.Responsibilities["invariant-transformation"] {
				t.Fatalf("grouped receiver differs: %+v / %+v", a, b)
			}
		})
	}
	goSource := "package sample\ntype Example struct{value int}\nfunc(e *Example)Run(x int)int{alias:=e\nalias.value+=x\nreturn alias.value}"
	got := structuralResult("go", "example.go", goSource).Grade
	if got == nil || got.Responsibilities["state"] != 0 || got.Responsibilities["invariant-transformation"] != 2 {
		t.Fatalf("newline alias lost ownership: %+v", got)
	}
}

func TestStructuralReviewRustMovedGuards(t *testing.T) {
	prefix := `struct Driver{active:bool}impl Driver{fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self){let guard=Guard(&mut self.d);`
	for _, tail := range []string{`let moved=guard;std::mem::forget(moved);`, `let aggregate=(guard,);std::mem::forget(aggregate);`} {
		got := structuralResult("rust", "lib.rs", prefix+tail+`}}`).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("moved guard cleanup invented: %+v", got)
		}
	}
}

func TestStructuralReviewUnresolvedCleanupContradictions(t *testing.T) {
	for _, body := range []string{`d.begin();try{}finally{d.end();}return d.use();`, `d.begin();d=new Driver();try{return d.use();}finally{d.end();}`, `d.begin();if(flag){try{return d.use();}finally{d.end();}}return d.use();`, `d.begin();try{return d.use();}finally{while(flag){d.end();}}`} {
		got := structuralResult("java", "Example.java", `public class Example{private Driver d=new Driver();public int run(boolean flag){`+body+`}}`).Grade
		if got == nil {
			t.Fatal("missing grade")
		}
		for _, limit := range got.MaterialLimitations {
			if limit == "unresolved_connected_cleanup" {
				t.Fatalf("contradictory cleanup estimated: %s %+v", body, got)
			}
		}
	}
}

func TestStructuralReviewValidationDecoys(t *testing.T) {
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `public class Example{public int run(int x,int a,int b,int c){int require=0;int panic=0;int raise=0;return x+a+b+c;}}`},
		{"typescript", "example.ts", `export function run(x:number,a:number,b:number,c:number){let require=0;let panic=0;let raise=0;return x+a+b+c;}`},
		{"go", "example.go", `package p;func Run(x,a,b,c int)int{require:=0;panic:=0;raise:=0;_ = require;_ = panic;_ = raise;return x+a+b+c}`},
		{"rust", "lib.rs", `pub fn run(x:i32,a:i32,b:i32,c:i32)->i32{let require=0;let panic=0;let raise=0;x+a+b+c}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			a := structuralResult(tc.language, tc.path, tc.source).Grade
			b := structuralResult(tc.language, tc.path, strings.NewReplacer("require", "alpha", "panic", "beta", "raise", "gamma").Replace(tc.source)).Grade
			if a == nil || b == nil || a.Responsibilities["validation"] != 0 || b.Responsibilities["validation"] != 0 || a.Hidden != b.Hidden || a.Value() != b.Value() {
				t.Fatalf("bare validation identifiers affect duty: %+v / %+v", a, b)
			}
		})
	}
	got := structuralResult("go", "example.go", `package p;func Run(x int,panic func(string)){if x<0 {panic("bad")}}`).Grade
	if got == nil || got.Responsibilities["validation"] != 0 {
		t.Fatalf("shadowed panic accepted as builtin: %+v", got)
	}
}

func TestStructuralReviewStorageHelperBinding(t *testing.T) {
	variants := []string{
		`public class Example{private int value;public int run(){value+=1;return value;}}`,
		`public class Example{private int value;private void bump(int n){value+=n;}public int run(){bump(1);return value;}}`,
		`public class Example{private int value;public int run(){this.value++;return this.value;}}`,
		`public class Example{private int value;public int run(){(this).value=(this).value+1;return (this).value;}}`,
	}
	profile := DefaultCalibration()
	profile.State = 1.6
	profile.InvariantTransformation = 2.4
	var first *GradedEvidence
	for _, source := range variants {
		results, err := AnalyzeWithCalibration([]File{{Language: "java", Path: "Example.java", Source: []byte(source)}}, profile)
		if err != nil {
			t.Fatal(err)
		}
		got := results["Example.java"].Grade
		if first == nil {
			first = got
		}
		if got == nil || got.Hidden != first.Hidden || got.Value() != first.Value() || got.Responsibilities["state"] != profile.State || got.Responsibilities["invariant-transformation"] != 0 {
			t.Fatalf("binding/syntax altered storage classification: %+v / %+v", first, got)
		}
	}
}

func TestStructuralReviewDiscardedProtocolRead(t *testing.T) {
	got := structuralResult("java", "Example.java", `class Driver{boolean active;void begin(){active=true;}int use(){boolean ignored=active;return 1;}void end(){active=false;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`).Grade
	if got == nil || got.Responsibilities["resource"] != 0 {
		t.Fatalf("discarded protocol read established resource use: %+v", got)
	}
}

func TestStructuralReviewOuterAliasMutations(t *testing.T) {
	prefix := `public class Example{private int value;public void run(int x,boolean flag,Example other){Example alias=this;`
	for _, body := range []string{`if(flag){alias=other;}alias.value+=x;`, `{alias=other;}alias.value+=x;`} {
		got := structuralResult("java", "Example.java", prefix+body+`}}`).Grade
		if got == nil || got.Responsibilities["invariant-transformation"] != 0 {
			t.Fatalf("outer alias mutation ignored: %s %+v", body, got)
		}
	}
	// An inner declaration and its mutation belong to a separate binding.
	got := structuralResult("typescript", "example.ts", `export class Example{private value=0;run(x:number,other:Example){let alias=this;{let alias=other;alias=other;}alias.value+=x;}}`).Grade
	if got == nil || got.Responsibilities["invariant-transformation"] != 2 {
		t.Fatalf("inner shadow erased outer binding: %+v", got)
	}
}

func TestStructuralReviewCompoundBooleanResetWrites(t *testing.T) {
	for _, operator := range []string{"^=", "|=", "&=", "^ =", "| =", "& ="} {
		source := `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;active` + operator + `true;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`
		got := structuralResult("java", "Example.java", source).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("compound %s preserves false reset proof: %+v", operator, got)
		}
	}
}

func TestStructuralReviewCatchPreservesCleanup(t *testing.T) {
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`},
		{"typescript", "example.ts", `class Driver{active=false;begin(){this.active=true;}use(){if(!this.active)throw new Error();return 1;}end(){this.active=false;}}export class Example{private d=new Driver();run(){this.d.begin();try{return this.d.use();}finally{this.d.end();}}}`},
	} {
		a := structuralResult(tc.language, tc.path, tc.source).Grade
		catch := `catch(RuntimeException e){return -1;}`
		if tc.language == "typescript" {
			catch = `catch(e){return -1;}`
		}
		b := structuralResult(tc.language, tc.path, strings.ReplaceAll(tc.source, "finally", catch+"finally")).Grade
		if a == nil || b == nil || a.Responsibilities["resource"] != 2 || b.Responsibilities["resource"] != 2 {
			t.Fatalf("catch lost cleanup: %+v / %+v", a, b)
		}
	}
}

func TestStructuralReviewRustObservableUse(t *testing.T) {
	for _, use := range []string{`1`, `let ignored=self.active;1`} {
		source := `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{` + use + `}fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);let out=guard.0.use_value();drop(guard);out}}`
		got := structuralResult("rust", "lib.rs", source).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("unobserved captured resource use: %+v", got)
		}
	}
}

func TestStructuralReviewResetFollowedByCall(t *testing.T) {
	for _, call := range []string{`begin();`, `unknown();`} {
		source := `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;` + call + `}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`
		got := structuralResult("java", "Example.java", source).Grade
		if got == nil || got.Responsibilities["resource"] != 0 {
			t.Fatalf("later call retained final reset proof: %+v", got)
		}
	}
	source := `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn end(&mut self){self.active=false;self.begin();}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self){let guard=Guard(&mut self.d);drop(guard);}}`
	got := structuralResult("rust", "lib.rs", source).Grade
	if got == nil || got.Responsibilities["resource"] != 0 {
		t.Fatalf("Rust reacquisition retained final reset: %+v", got)
	}
}
