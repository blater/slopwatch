package sourceestimate

import (
	"math"
	"strings"
	"testing"
)

func TestGradedPrivateHelperUnavailable(t *testing.T) {
	for _, c := range []struct{ language, path, helperPath, source, helper string }{
		{"java", "Example.java", "Worker.java", `public class Example { public int process(int a,int b,int c){return Worker.process(a,b,c);} }`, `final class Worker { static int process(int a,int b,int c){if(a<0||b<0||c<0)throw new IllegalArgumentException();return a+b+c;} }`},
		{"go", "example.go", "worker.go", `package p; func Process(a,b,c int)int{return work(a,b,c)}`, `package p; func work(a,b,c int)int{if a<0||b<0||c<0 {panic("negative")};return a+b+c}`},
		{"typescript", "example.ts", "worker.ts", `import {work} from './worker'; export function process(a:number,b:number,c:number){return work(a,b,c);}`, `export function work(a:number,b:number,c:number){if(a<0||b<0||c<0)throw new Error();return a+b+c;}`},
		{"rust", "lib.rs", "worker.rs", `mod worker; pub fn process(a:i32,b:i32,c:i32)->i32{worker::work(a,b,c)}`, `pub(crate) fn work(a:i32,b:i32,c:i32)->i32{assert!(a>=0&&b>=0&&c>=0);a+b+c}`},
	} {
		t.Run(c.language, func(t *testing.T) {
			file := File{Path: c.path, Language: c.language, Source: []byte(c.source)}
			helper := File{Path: c.helperPath, Language: c.language, Source: []byte(c.helper)}
			_, full := AnalyzeWithAttribution([]File{file, helper})
			_, missing := AnalyzeWithAttribution([]File{file})
			before, after := full[c.path].PublishedPenalty(), missing[c.path].PublishedPenalty()
			if !full[c.path].Applicable || !missing[c.path].Applicable || after <= 0 || after > before || math.Abs(after-before) > 10 {
				t.Fatalf("missing helper changed graded assessment %v -> %v; before=%+v after=%+v", before, after, full[c.path].Grade, missing[c.path].Grade)
			}
			if missing[c.path].Grade.EstimatedHidden == 0 || len(missing[c.path].Limitations) == 0 {
				t.Fatal("estimated duties lost uncertainty metadata")
			}
		})
	}
}

func TestGradedUnknownResultSyntaxInvariant(t *testing.T) {
	var first float64
	for i, body := range []string{`return Worker.process(a,b,c);`, `return Worker.process((a),b,c);`, `int result=Worker.process(a,b,c);return result;`} {
		_, results := AnalyzeWithAttribution([]File{{Path: "Example.java", Language: "java", Source: []byte(`public class Example{public int process(int a,int b,int c){` + body + `}}`)}})
		r := results["Example.java"]
		if i == 0 {
			first = r.PublishedPenalty()
		}
		if r.PublishedPenalty() != first || r.Grade.EstimatedHidden == 0 {
			t.Fatalf("harmless result syntax changed estimate: %s => %+v", body, r.Grade)
		}
	}
}

func TestGradedTransparentProtocolGuardGrouping(t *testing.T) {
	wrapper := File{Path: "Example.java", Language: "java", Source: []byte(`public class Example{private Driver d=new Driver();public void acquire(String c){d.acquire(c);}public void append(int v){d.append(v);}public int finish(){return d.finish();}public void release(){d.release();}}`)}
	driver := `public class Driver{boolean a;int s;public void acquire(String c){GUARD a=true;s=0;}public void append(int v){if(!a)throw new IllegalStateException();s+=v;}public int finish(){if(!a)throw new IllegalStateException();return s;}public void release(){if(!a)throw new IllegalStateException();a=false;}}`
	var values []float64
	for _, guard := range []string{`if(a||c.isEmpty())throw new IllegalStateException();`, `if(a)throw new IllegalStateException();if(c.isEmpty())throw new IllegalStateException();`} {
		_, r := AnalyzeWithAttribution([]File{wrapper, {Path: "Driver.java", Language: "java", Source: []byte(strings.ReplaceAll(driver, "GUARD", guard))}})
		values = append(values, r[wrapper.Path].PublishedPenalty())
	}
	if values[0] < 65 || math.Abs(values[0]-values[1]) > 3 {
		t.Fatalf("equivalent transparent protocol scores=%v", values)
	}
}

func TestGradedUnknownGuardPreservesReturnedDuty(t *testing.T) {
	for _, c := range []struct{ language, path, prefix, suffix, guard, direct, local string }{
		{"java", "Example.java", `public class Example{public String process(String x,int a,int b,int c){`, `}}`, `if(x==null)return "";`, `return External.convert((x),a,b,c);`, `String result=External.convert(x,a,b,c);return result;`},
		{"typescript", "example.ts", `export function process(x:string,a:number,b:number,c:number){`, `}`, `if(x==null)return "";`, `return external.convert((x),a,b,c);`, `const result=external.convert(x,a,b,c);return result;`},
		{"go", "example.go", `package p;func Process(x string,a,b,c int)string{`, `}`, `if x=="" {return ""};`, `return external.Convert((x),a,b,c)`, `result:=external.Convert(x,a,b,c);return result`},
		{"rust", "lib.rs", `pub fn process(x:&str,a:i32,b:i32,c:i32)->&str{`, `}`, `if x=="" {return "";}`, `external::convert((x),a,b,c)`, `let result=external::convert(x,a,b,c);result`},
	} {
		t.Run(c.language, func(t *testing.T) {
			analyze := func(body string) Result {
				_, r := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.prefix + body + c.suffix)}})
				return r[c.path]
			}
			bare := analyze(c.direct)
			for _, body := range []string{c.guard + c.direct, c.guard + c.local} {
				guarded := analyze(body)
				if bare.Grade.EstimatedHidden != 3 || guarded.Grade.EstimatedHidden != 3-guarded.Grade.Responsibilities["validation"] || (c.language != "rust" && guarded.Grade.Responsibilities["validation"] != 1) || guarded.PublishedPenalty() > bare.PublishedPenalty() || len(guarded.Limitations) == 0 {
					t.Fatalf("guard changed returned duty: bare=%+v guarded=%+v penalty=%v -> %v", bare.Grade, guarded.Grade, bare.PublishedPenalty(), guarded.PublishedPenalty())
				}
			}
		})
	}
}

func TestGradedUnknownDisconnectedResult(t *testing.T) {
	for _, c := range []struct{ language, path, prefix, suffix, call, assign, ret, overwrite string }{
		{"java", "Example.java", `public class Example{public int process(int a,int b,int c){`, `}}`, `External.convert(a,b,c);`, `int result=External.convert(a,b,c);`, `return result;`, `result=0;`},
		{"typescript", "example.ts", `export function process(a:number,b:number,c:number){`, `}`, `external.convert(a,b,c);`, `let result=external.convert(a,b,c);`, `return result;`, `result=0;`},
		{"go", "example.go", `package p;func Process(a,b,c int)int{`, `}`, `external.Convert(a,b,c);`, `result:=external.Convert(a,b,c);`, `return result`, `result=0;`},
		{"rust", "lib.rs", `pub fn process(a:i32,b:i32,c:i32)->i32{`, `}`, `external::convert(a,b,c);`, `let mut result=external::convert(a,b,c);`, `result`, `result=0;`},
	} {
		t.Run(c.language, func(t *testing.T) {
			for _, body := range []string{c.call + `return 0;`, c.assign + c.overwrite + c.ret} {
				_, r := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.prefix + body + c.suffix)}})
				if r[c.path].Grade.EstimatedHidden != 0 {
					t.Fatalf("disconnected result received allowance: %s => %+v", body, r[c.path].Grade)
				}
			}
		})
	}
}

func TestGradedUnknownResultEligibilityBoundaries(t *testing.T) {
	for _, c := range []struct {
		name, body string
		eligible   bool
	}{
		{"throwing guard", `if(a<0)throw new IllegalArgumentException();return External.convert(a,b,c);`, true},
		{"returned parentheses", `if(a<0)return 0;return (External.convert(a,b,c));`, true},
		{"dead call", `if(false){return External.convert(a,b,c);}return 0;`, false},
		{"closure", `Supplier<Integer> result=()->{return External.convert(a,b,c);};return 0;`, false},
		{"field not local", `this.result=External.convert(a,b,c);return result;`, false},
		{"overwritten alias", `int result=External.convert(a,b,c);result=0;return result;`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			body, _, _ := lex([]byte(c.body))
			op := &operation{language: "java", name: "process", paramNames: []string{"a", "b", "c"}, body: body}
			if _, ok := gradeUnresolvedResultCall(op); ok != c.eligible {
				t.Fatalf("eligibility=%v want %v", ok, c.eligible)
			}
			if c.eligible {
				if _, ok := gradeTransparentCall(op); ok {
					t.Fatal("validation relaxed protocol transparency")
				}
			}
		})
	}
	_, r := AnalyzeWithAttribution([]File{{Path: "Example.java", Language: "java", Source: []byte(`public class Example{private Driver d=new Driver();public int process(int a,int b,int c){if(a<0)return 0;return d.convert(a,b,c);}}`)}})
	if r["Example.java"].Grade.EstimatedHidden != 0 {
		t.Fatalf("owned delegate received returned-duty allowance: %+v", r["Example.java"].Grade)
	}
}

func TestGradedUnknownResultWrapperSyntax(t *testing.T) {
	for _, c := range []struct {
		language, path, prefix, suffix, guard string
		bodies                                []string
	}{
		{"java", "Example.java", `public class Example{public String process(String x,int a,int b,int c){`, `}}`, `if(x==null)return "";`, []string{`return External.convert(x,a,b,c);`, `return (String) External.convert(x,a,b,c);`, `String result=External.convert(x,a,b,c);return (result);`}},
		{"typescript", "example.ts", `export async function process(x:string,a:number,b:number,c:number){`, `}`, `if(x==null)return "";`, []string{`return external.convert(x,a,b,c);`, `return await external.convert(x,a,b,c);`, `return external.convert(x,a,b,c) as string;`, `const result=external.convert(x,a,b,c);return (result);`, `const unused=0
const result=external.convert(x,a,b,c)
return (result)`, `const result:string=await external.convert(x,a,b,c);return (result);`}},
		{"go", "example.go", `package p;func Process(x string,a,b,c int)string{`, `}`, `if x=="" {return ""};`, []string{`return external.Convert(x,a,b,c)`, `result:=external.Convert(x,a,b,c);return (result)`}},
		{"rust", "lib.rs", `pub fn process(x:&str,a:i32,b:i32,c:i32)->&str{`, `}`, `if x=="" {return "";}`, []string{`external::convert(x,a,b,c)`, `let result=external::convert(x,a,b,c);(result)`}},
	} {
		t.Run(c.language, func(t *testing.T) {
			var baseline float64
			for _, guard := range []string{"", c.guard} {
				for i, body := range c.bodies {
					_, r := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.prefix + guard + body + c.suffix)}})
					got := r[c.path]
					if guard == "" && i == 0 {
						baseline = got.PublishedPenalty()
					}
					if got.Grade.EstimatedHidden == 0 || got.PublishedPenalty() != baseline {
						t.Fatalf("result syntax lost duty: %s%s => %+v penalty=%v want=%v", guard, body, got.Grade, got.PublishedPenalty(), baseline)
					}
				}
			}
		})
	}
}

func TestGradedUnknownArrowResultSyntax(t *testing.T) {
	for _, c := range []struct {
		source string
		want   float64
	}{
		{`export const process=(a:number,b:number,c:number)=>external.convert(a,b,c);`, 3},
		{`export const process=async(a:number,b:number,c:number)=>await external.convert(a,b,c);`, 3},
		{`export const process=(a:number,b:number,c:number)=>external.convert(a,b,c) as string;`, 3},
		{`export const process=(a:number,b:number,c:number)=>{return external.convert(a,b,c);};`, 3},
		{`export const process=(a:number,b:number,c:number)=>{external.convert(a,b,c);};`, 0},
	} {
		_, r := AnalyzeWithAttribution([]File{{Path: "example.ts", Language: "typescript", Source: []byte(c.source)}})
		if r["example.ts"].Grade.EstimatedHidden != c.want {
			t.Fatalf("arrow result duty: %s => %+v want=%v", c.source, r["example.ts"].Grade, c.want)
		}
	}
}

func TestGradedUnknownRequiresLocalBinding(t *testing.T) {
	for _, c := range []struct{ language, body string }{
		{"java", `result=External.convert(a,b,c);return result;`},
		{"java", `if(a>0)return External.convert(a,b,c);`},
		{"java", `if(a>0){return External.convert(a,b,c);}`},
		{"java", `if(a>0)result=External.convert(a,b,c);return result;`},
		{"java", `int result=0;if(a>0)result=External.convert(a,b,c);return result;`},
		{"typescript", `let result=0;if(a>0)result=external.convert(a,b,c);return result;`},
		{"typescript", `if(a>0)var result=external.convert(a,b,c);return result;`},
	} {
		body, _, _ := lex([]byte(c.body))
		op := &operation{language: c.language, name: "process", paramNames: []string{"a", "b", "c"}, body: body}
		if _, ok := gradeUnresolvedResultCall(op); ok {
			t.Fatalf("nonlocal or conditional binding accepted: %s", c.body)
		}
	}
}
