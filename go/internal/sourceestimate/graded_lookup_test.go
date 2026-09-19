package sourceestimate

import (
	"os"
	"strings"
	"testing"
)

func TestGradedActualXMLToasterLookup(t *testing.T) {
	source, err := os.ReadFile("testdata/xmltoaster/QueryResultRow.java")
	if err != nil {
		t.Fatal(err)
	}
	_, results := AnalyzeWithAttribution([]File{{Path: "QueryResultRow.java", Language: "java", Source: source}})
	r := results["QueryResultRow.java"]
	if r.Grade == nil || r.PublishedPenalty() < 15 || r.PublishedPenalty() > 45 || r.Grade.EstimatedHidden <= 0 || r.Grade.Hidden != 0 || r.Grade.ResidualBurden != 4 || r.Grade.SupportedBurden != 4 || len(r.Limitations) == 0 {
		t.Fatalf("lookup responsibility not preserved: result=%+v grade=%+v", r, r.Grade)
	}
}

func TestGradedConnectedLookup(t *testing.T) {
	for _, c := range []struct{ language, path, prefix, suffix, bind, ret, guard, closure string }{
		{"java", "Example.java", `public class Example{private Store store;public String process(String key,int a,int b){`, `}}`, `var value=store.get(key);`, `return value==null ? "" : value.render();`, `if(key==null)return "";`, `Supplier<String> ignored=()->{return store.get(key).render();};return "";`},
		{"typescript", "example.ts", `export class Example{private store:Store;process(key:string,a:number,b:number){`, `}}`, `let value=this.store.get(key);`, `return value==null ? "" : value.render();`, `if(key==null)return "";`, `const ignored=()=>this.store.get(key).render();return "";`},
		{"go", "example.go", `package p;type Example struct{store Store};func(e *Example) Process(key string,a,b int)string{`, `}`, `value:=e.store.Get(key);`, `return value.Render()`, `if key=="" {return ""};`, `ignored:=func()string{return e.store.Get(key).Render()};return "";`},
		{"rust", "lib.rs", `pub struct Example{store:Store} impl Example{pub fn process(&self,key:&str,a:i32,b:i32)->String{`, `}}`, `let mut value=self.store.get(key);`, `value.render()`, `if key=="" {return String::new();}`, `let ignored=||{self.store.get(key).render()};return String::new();`},
	} {
		t.Run(c.language, func(t *testing.T) {
			analyze := func(body string) Result {
				if c.language == "rust" {
					body = strings.ReplaceAll(body, `return "";`, `return String::new();`)
				}
				_, r := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.prefix + body + c.suffix)}})
				return r[c.path]
			}
			bare := analyze(c.bind + c.ret)
			guarded := analyze(c.guard + c.bind + c.ret)
			if bare.Grade.EstimatedHidden != 3 || guarded.Grade.EstimatedHidden != 3-guarded.Grade.Responsibilities["validation"] || guarded.PublishedPenalty() > bare.PublishedPenalty() {
				t.Fatalf("connected result/guard lost: bare=%+v guarded=%+v", bare.Grade, guarded.Grade)
			}
			overwrite := `value=null;`
			if c.language == "go" {
				overwrite = `value=nil;`
			}
			if c.language == "rust" {
				overwrite = `value=Entry::empty();`
			}
			for _, body := range []string{c.bind + `return "";`, c.bind + overwrite + c.ret, c.closure, c.bind + `if(false){` + c.ret + `;};return "";`} {
				r := analyze(body)
				if r.Grade.EstimatedHidden != 0 {
					t.Fatalf("disconnected lookup credited: %s => %+v", body, r.Grade)
				}
			}
		})
	}
}

func TestGradedLookupCompositionEnvelope(t *testing.T) {
	for _, body := range []string{
		`var value=store.get(key);return Formatter.render(value);`,
		`return store.get(key).render();`,
		`var value=store.get(key);var alias=value;return alias.render();`,
		`var value=store.get(key);var other=store.get(key);return Formatter.render(value,other);`,
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: "Example.java", Language: "java", Source: []byte(`public class Example{Store store;public Object process(String key,int a,int b){` + body + `}}`)}})
		if g := results["Example.java"].Grade; g.EstimatedHidden != 3 {
			t.Fatalf("composition lost or multiplied allowance: %s => %+v", body, g)
		}
	}
	for _, category := range []string{"transform", "representation-transformation", "invariant-transformation"} {
		if got := gradeResultDutyEnvelope(map[string]float64{category: 2, "validation": 1}); got != 0 {
			t.Fatalf("%s double counted: %v", category, got)
		}
	}
}

func TestGradedLookupPredicateOverlap(t *testing.T) {
	for _, c := range []struct {
		body, helper string
		want         float64
	}{
		{`var value=store.get(key);return value==null || value.empty();`, ``, 1},
		{`if(key==null)return false;var value=store.get(key);return value==null || value.empty();`, ``, 0},
		{`var value=store.get(key);var result=(value==null);return result;`, ``, 1},
		{`var value=store.get(key);return check(value);`, `private boolean check(Object value){return value==null || value.empty();}`, 1},
		{`var value=store.get(key);return value==null ? false : true;`, ``, 1},
		{`var value=store.get(key);return value==null ? "" : value.render();`, ``, 3},
		{`var value=store.get(key);return Formatter.render(value==null);`, ``, 3},
		{`var value=store.get(key);var result=value==null;return result.toString();`, ``, 3},
		{`var value=store.get(key);return (value==null ? "" : value.render());`, ``, 3},
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: "Example.java", Language: "java", Source: []byte(`public class Example{Store store;public Object process(String key,int a,int b){` + c.body + `}` + c.helper + `}`)}})
		if g := results["Example.java"].Grade; g.EstimatedHidden != c.want {
			t.Fatalf("predicate category overlapped transformation: %s => %+v want=%v", c.body, g, c.want)
		}
	}
}

func TestGradedLookupPredicateLanguages(t *testing.T) {
	for _, c := range []struct{ language, path, source string }{
		{"java", "Example.java", `public class Example{public boolean present(String key,int a,int b){var value=External.get(key);return value!=null;}}`},
		{"typescript", "example.ts", `export function present(key:string,a:number,b:number){const value=external.get(key);return value!==null;}`},
		{"go", "example.go", `package p;func Present(key string,a,b int)bool{value:=external.Get(key);return value!=nil}`},
		{"rust", "lib.rs", `pub fn present(key:&str,a:i32,b:i32)->bool{let value=external::get(key);value!=0}`},
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.source)}})
		if g := results[c.path].Grade; g.EstimatedHidden != 1 {
			t.Fatalf("%s predicate received nonvalidation duty: %+v", c.language, g)
		}
	}
}

func TestGradedLookupValidatorKeepsRecognizedValidation(t *testing.T) {
	var files []File
	for _, name := range []string{"SqlDerivedReferenceValidator.java", "SqlDerivedPredicateReferences.java", "SqlDerivedColumnResolver.java"} {
		source, err := os.ReadFile("../../../docs/evidence/shallow-v4/graded-real/java-derived-reference-validator/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, File{Path: name, Language: "java", Source: source})
	}
	_, results := AnalyzeWithAttribution(files)
	r := results["SqlDerivedReferenceValidator.java"]
	if r.PublishedPenalty() != 7 || r.Grade.EstimatedHidden != 0 || r.Grade.Responsibilities["validation"] != 3 {
		t.Fatalf("predicate helpers duplicated validation: penalty=%v grade=%+v", r.PublishedPenalty(), r.Grade)
	}
}

func TestGradedLookupOwnedProtocolRelay(t *testing.T) {
	for _, c := range []struct{ language, path, source string }{
		{"java", "Example.java", `public class Example{Driver driver;public void begin(String key){driver.begin(key);}public Object read(String key){return driver.read(key);}public void close(){driver.close();}}`},
		{"typescript", "example.ts", `export class Example{private driver:Driver;begin(key:string){this.driver.begin(key);}read(key:string){return this.driver.read(key);}close(){this.driver.close();}}`},
		{"go", "example.go", `package p;type Example struct{driver Driver};func(e *Example)Begin(key string){e.driver.Begin(key)};func(e *Example)Read(key string)any{return e.driver.Read(key)};func(e *Example)Close(){e.driver.Close()}`},
		{"rust", "lib.rs", `pub struct Example{driver:Driver} impl Example{pub fn begin(&mut self,key:&str){self.driver.begin(key);}pub fn read(&self,key:&str)->Value{self.driver.read(key)}pub fn close(&mut self){self.driver.close();}}`},
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.source)}})
		if g := results[c.path].Grade; g.EstimatedHidden != 0 {
			t.Fatalf("%s owned relay claimed delegate work: %+v", c.language, g)
		}
	}
}

func TestGradedConnectedLookupSemicolonless(t *testing.T) {
	for _, c := range []struct{ language, path, source string }{
		{"go", "example.go", "package p;func Process(key string,a,b int)string{value:=external.Get(key)\nreturn value.Render()}"},
		{"typescript", "example.ts", "export function process(key:string,a:number,b:number){const value=external.get(key)\nconst alias=value\nreturn alias.render()}"},
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.source)}})
		if g := results[c.path].Grade; g.EstimatedHidden != 3 {
			t.Fatalf("%s semicolonless flow lost: %+v", c.language, g)
		}
	}
}

func TestGradedLookupResultDependence(t *testing.T) {
	for _, c := range []struct {
		body, helper string
		want         float64
	}{
		{`var value=store.get(key);return constant(value);`, `private int constant(Object ignored){return 0;}`, 0},
		{`var value=store.get(key);return identity(value);`, `private Object identity(Object value){return value;}`, 0},
		{`return store.get(key)+0;`, ``, 0},
		{`return store.get(key)*1;`, ``, 0},
		{`return false && external.get(key);`, ``, 0},
		{`return true || external.get(key);`, ``, 0},
		{`return external.get(key) ? 0 : 0;`, ``, 0},
		{`var value=lookup(key);return value.render();`, `private Object lookup(String key){return store.get(key);}`, 3},
		{`var value=store.get(key);return value.render();`, ``, 3},
		{`var value=store.get(key);value=0;return value.render();`, ``, 0},
		{`var value=store.get(key);if(key==null){value=0;}return value.render();`, ``, 0},
	} {
		_, results := AnalyzeWithAttribution([]File{{Path: "Example.java", Language: "java", Source: []byte(`public class Example{Store store;public Object process(String key,int a,int b){` + c.body + `}` + c.helper + `}`)}})
		if g := results["Example.java"].Grade; g.EstimatedHidden != c.want {
			t.Fatalf("result dependence wrong: %s => %+v want=%v", c.body, g, c.want)
		}
	}
}

func TestGradedLookupFlowBoundaries(t *testing.T) {
	for _, c := range []struct {
		name, language, path, source string
		want                         float64
	}{
		{"live OR after dead AND", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return false && ignored() || value.render()=="";}}`, 1},
		{"live TS OR after dead AND", "typescript", "example.ts", `export function process(key:string,a:number,b:number){const value=external.get(key);return false && ignored() || value.render();}`, 3},
		{"dead AND within live OR", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return value.render()=="" || false && ignored();}}`, 1},

		{"unreachable true arm", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return false ? value.render() : "";}}`, 0},
		{"unreachable false arm", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return (true) ? "" : value.render();}}`, 0},
		{"equivalent connected arms", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return key==null ? value.render() : value.render();}}`, 3},
		{"equivalent relay arms", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);return key==null ? value : value;}}`, 0},
		{"else overwrite", "java", "Example.java", `public class Example{Store store;public Object process(String key,int a,int b){var value=store.get(key);if(key==null){}else{value=null;}return value.render();}}`, 0},
		{"parallel overwrite", "go", "example.go", `package p;func Process(key string,a,b int)string{value:=external.Get(key);other:=external.Empty();value,other=nil,nil;return value.Render()}`, 0},
		{"operand selection", "typescript", "example.ts", `export function process(key:string,a:number,b:number){const value=external.get(key);return value || "fallback";}`, 3},
		{"boolean selection", "typescript", "example.ts", `export function process(key:string,a:number,b:number){const value=external.get(key);return value!==null || key!==null;}`, 1},
		{"semicolonless Go aliases", "go", "example.go", "package p;func Process(key string,a,b int)string{value:=external.Get(key)\nalias:=value\nreturn alias.Render()}", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, results := AnalyzeWithAttribution([]File{{Path: c.path, Language: c.language, Source: []byte(c.source)}})
			if g := results[c.path].Grade; g == nil || g.EstimatedHidden != c.want {
				t.Fatalf("flow boundary: grade=%+v want=%v", g, c.want)
			}
		})
	}
}
