package sourceestimate

import "testing"

func surfaceFindingFor(t *testing.T, files []File, path string) Finding {
	t.Helper()
	_, results := AnalyzeWithAttribution(files)
	got := results[path].Findings
	if len(got) != 1 {
		t.Fatalf("expected one source surface finding for %s, got %+v (result=%+v)", path, got, results[path])
	}
	return got[0]
}

func TestSourceSurfaceInputsJavaPositiveAndCallerWitness(t *testing.T) {
	finding := surfaceFindingFor(t, []File{
		{Path: "Service.java", Language: "java", Source: []byte(`class Service {
  public static int run(int unused, int used) { return used * 2; }
}`)},
		{Path: "Caller.java", Language: "java", Source: []byte(`class Caller {
  int call(int input) { return Service.run(input + 1, input); }
}`)},
	}, "Service.java")
	if finding.Kind != "unused-input" || finding.Parameter != "unused" || finding.UnnecessaryBurden != 0.25 {
		t.Fatalf("unexpected Java finding: %+v", finding)
	}
	if len(finding.CallerFiles) != 1 || finding.CallerFiles[0] != "Caller.java" {
		t.Fatalf("expected observed Java caller witness: %+v", finding)
	}
}

func TestSourceSurfaceInputsPublishesSourceInferredForExposedOperation(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "inferred.ts", Language: "typescript", Source: []byte(`
export function run(unused: number, used: number) { return used * 2; }
`)}})
	findings := results["inferred.ts"].Findings
	if len(findings) != 1 || findings[0].Parameter != "unused" || len(findings[0].CallerFiles) != 0 {
		t.Fatalf("expected source-inferred finding without caller witness: %+v", results["inferred.ts"])
	}
}

func TestSourceSurfaceInputsTypeScriptPositiveAndNegatives(t *testing.T) {
	positive := surfaceFindingFor(t, []File{{Path: "service.ts", Language: "typescript", Source: []byte(`
export function run(unused: number, used: number) { return used * 2; }
export function call(input: number) { return run(input + 1, input); }
`)}}, "service.ts")
	if positive.Parameter != "unused" || len(positive.CallerFiles) != 1 || positive.CallerFiles[0] != "service.ts" {
		t.Fatalf("unexpected TypeScript finding: %+v", positive)
	}

	_, identity := AnalyzeWithAttribution([]File{{Path: "identity.ts", Language: "typescript", Source: []byte(`
export function identity(used: number) { return used; }
export function call(input: number) { return identity(input); }
`)}})
	if len(identity["identity.ts"].Findings) != 0 {
		t.Fatalf("pure identity should not produce a finding: %+v", identity["identity.ts"])
	}
}

func TestSourceSurfaceInputsRustPositiveAndTraitNegative(t *testing.T) {
	positive := surfaceFindingFor(t, []File{{Path: "service.rs", Language: "rust", Source: []byte(`
pub fn run(unused: i32, used: i32) -> i32 { used * 2 }
pub fn call(input: i32) -> i32 { run(input + 1, input) }
`)}}, "service.rs")
	if positive.Parameter != "unused" || len(positive.CallerFiles) != 1 || positive.CallerFiles[0] != "service.rs" {
		t.Fatalf("unexpected Rust finding: %+v", positive)
	}

	_, traitResult := AnalyzeWithAttribution([]File{{Path: "trait.rs", Language: "rust", Source: []byte(`
pub trait Service { fn run(&self, unused: i32, used: i32) -> i32; }
struct Impl;
impl Service for Impl { fn run(&self, unused: i32, used: i32) -> i32 { used * 2 } }
pub fn call(input: i32) -> i32 { 1 }
`)}})
	if len(traitResult["trait.rs"].Findings) != 0 {
		t.Fatalf("trait implementation should not produce a finding: %+v", traitResult["trait.rs"])
	}
}

func TestSourceSurfaceInputsRejectsContractsAndOptionalParameters(t *testing.T) {
	_, java := AnalyzeWithAttribution([]File{{Path: "Contract.java", Language: "java", Source: []byte(`
interface Contract { int run(int unused, int used); }
class Impl implements Contract {
  public int run(int unused, int used) { return used * 2; }
}
class Caller { int call(int input) { return new Impl().run(input + 1, input); } }
`)}})
	if len(java["Contract.java"].Findings) != 0 {
		t.Fatalf("Java contract method should not produce a finding: %+v", java["Contract.java"])
	}

	_, ts := AnalyzeWithAttribution([]File{{Path: "optional.ts", Language: "typescript", Source: []byte(`
export function run(unused?: number, used: number = 1) { return used * 2; }
export function call(input: number) { return run(input, input); }
`)}})
	if len(ts["optional.ts"].Findings) != 0 {
		t.Fatalf("optional/default parameters should not produce a finding: %+v", ts["optional.ts"])
	}
}

func TestRustCallbackContractDoesNotBecomeUnusedInputFinding(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "callback.rs", Language: "rust", Source: []byte(`pub fn callback(unused:i32, used:i32)->i32{used*2} pub fn register(){ install(callback); }`)}})
	if len(results["callback.rs"].Findings) != 0 {
		t.Fatalf("callback contract penalized: %+v", results["callback.rs"])
	}
}

func TestFindingSupportSurvivesUnrelatedUnresolvedBehavior(t *testing.T) {
	var baseline float64
	for _, extra := range []string{"", "missing();"} {
		_, results := AnalyzeWithAttribution([]File{{Path: "service.ts", Language: "typescript", Source: []byte(`export function run(x:number,unused:number){` + extra + `return x*2;}`)}})
		result := results["service.ts"]
		if len(result.Findings) != 1 || result.PublishedPenalty() <= 0 {
			t.Fatalf("specific unused-input evidence suppressed: %+v", result)
		}
		if baseline == 0 {
			baseline = result.PublishedPenalty()
		} else if result.PublishedPenalty() != baseline {
			t.Fatalf("unrelated uncertainty changed finding severity: %v != %v", result.PublishedPenalty(), baseline)
		}
		if extra != "" && len(result.Limitations) == 0 {
			t.Fatal("unresolved behavior lost diagnostics")
		}
	}
}

func TestGenericTypeArgumentsAreNotCallerInputs(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "Service.java", Language: "java", Source: []byte(`public class Service { public int count(Map<String,List<String>> entries,int offset){return entries.size()+offset;} }`)}})
	if len(results["Service.java"].Findings) != 0 {
		t.Fatalf("generic type became unused input: %+v", results["Service.java"])
	}
}
