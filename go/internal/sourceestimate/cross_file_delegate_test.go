package sourceestimate

import "testing"

func TestTypeScriptImportedOwnedDelegateResolvesAcrossFiles(t *testing.T) {
	source := func(path, text string) File { return File{Path: path, Language: "typescript", Source: []byte(text)} }
	delegated := AnalyzeTypeScriptFiles([]File{
		source("driver.ts", `export class Driver{decode(x:number){return x*2;}}`),
		source("example.ts", `import { Driver } from "./driver"; export class Example{private d=new Driver(); run(x:number){return this.d.decode(x);}}`),
	})["example.ts"]
	if len(delegated.Dependencies) != 1 || delegated.Dependencies[0] != "driver.ts#decode/0" {
		t.Fatalf("TypeScript owned delegate was not resolved: %+v", delegated)
	}
}

func TestRustTypedOwnedDelegateResolvesAcrossFiles(t *testing.T) {
	delegated := Analyze([]File{
		{Path: "driver.rs", Language: "rust", Source: []byte(`pub struct Driver; impl Driver{pub fn decode(&self,x:i32)->i32{x*2}}`)},
		{Path: "lib.rs", Language: "rust", Source: []byte(`pub mod driver; use driver::Driver; pub struct Example{d:Driver} impl Example{pub fn run(&self,x:i32)->i32{self.d.decode(x)}}`)},
	})["lib.rs"]
	if len(delegated.Dependencies) != 1 || delegated.Dependencies[0] != "driver.rs#decode/0" {
		t.Fatalf("Rust owned delegate was not resolved: %+v", delegated)
	}
}

func TestCrossFileOwnedDelegateLeavesAmbiguousOwnerUnresolved(t *testing.T) {
	result := AnalyzeTypeScriptFiles([]File{
		{Path: "one.ts", Language: "typescript", Source: []byte(`export class Decoder{decode(x:number){return x*2;}}`)},
		{Path: "two.ts", Language: "typescript", Source: []byte(`export class Decoder{decode(x:number){return x*3;}}`)},
		{Path: "example.ts", Language: "typescript", Source: []byte(`import { Decoder } from "./one"; export class Example{private d=new Decoder(); run(x:number){return this.d.decode(x);}}`)},
	})["example.ts"]
	if len(result.Dependencies) != 0 {
		t.Fatalf("ambiguous owner was resolved: %+v", result)
	}
}

func TestTypeScriptPrivateHelperIsReachableButNotAnExternalRoot(t *testing.T) {
	result := AnalyzeTypeScriptFiles([]File{{Path: "example.ts", Language: "typescript", Source: []byte(`export class Example{private helper(x:number){return x*2;} run(x:number){return this.helper(x);}}`)}})["example.ts"]
	if len(result.Abstractions) != 1 || result.Abstractions[0].Name != "external:Example" {
		t.Fatalf("private helper became an external root: %+v", result)
	}
	if result.Hidden == 0 {
		t.Fatalf("private helper was not retained as reachable work: %+v", result)
	}
}
