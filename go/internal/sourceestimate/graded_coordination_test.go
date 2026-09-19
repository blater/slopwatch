package sourceestimate

import "testing"

func TestGradeLocalCoordinationRequiresUsefulDelegateFlow(t *testing.T) {
	u := gradedSurfaceUnit("java", "Example.java", `public class Example { private Driver d; public int process(int x) { d.start(x); return d.finish(); } public int flow() { int out = d.first(); d.second(out); return out; } public int reads() { return d.first() + d.second(); } public int utility() { return Utility.first(); } }`)
	ops := map[string]*operation{}
	for _, op := range u.ops {
		ops[op.name] = op
	}
	if !gradeLocalCoordination(ops["process"], u, ops["process"].body, "d") {
		t.Error("command followed by returned delegate result was not recognized")
	}
	if !gradeLocalCoordination(ops["flow"], u, ops["flow"].body, "d") {
		t.Error("delegate output-to-input dependency was not recognized")
	}
	if gradeLocalCoordination(ops["reads"], u, ops["reads"].body, "d") {
		t.Error("unrelated delegate reads were treated as coordination")
	}
	if gradeLocalCoordination(ops["utility"], u, ops["utility"].body, "Utility") {
		t.Error("static utility call was treated as owned coordination")
	}
}

func TestGradeGuaranteedCleanupRequiresActualCleanupCall(t *testing.T) {
	cases := []struct {
		name     string
		language string
		path     string
		source   string
	}{
		{
			name:     "java finally",
			language: "java",
			path:     "Example.java",
			source:   `class Driver { boolean active; void acquire() { active=true; } int finish() { return 1; } void release() { active=false; } void log() {} } class Example { private Driver d=new Driver(); int process() { d.acquire(); try { return d.finish(); } finally { d.release(); } } }`,
		},
		{
			name:     "typescript finally",
			language: "typescript",
			path:     "example.ts",
			source:   `class Driver { active=false; acquire() { this.active=true; } finish() { return 1; } release() { this.active=false; } log() {} } class Example { private d=new Driver(); process() { this.d.acquire(); try { return this.d.finish(); } finally { this.d.release(); } } }`,
		},
		{
			name:     "go defer",
			language: "go",
			path:     "example.go",
			source:   `package sample; type Driver struct { active bool }; func (d *Driver) Acquire() { d.active=true }; func (d *Driver) Release() { d.active=false }; type Example struct { d Driver }; func (e *Example) Process() { e.d.Acquire(); defer e.d.Release() }`,
		},
		{
			name:     "rust guard",
			language: "rust",
			path:     "lib.rs",
			source:   `pub struct Driver { active:bool } impl Driver { pub fn release(&mut self) { self.active=false } } struct Release<'a>(&'a mut Driver); impl Drop for Release<'_> { fn drop(&mut self) { self.0.release() } } pub struct Example { d:Driver } impl Example { pub fn process(&mut self) { let guard=Release(&mut self.d); drop(guard); } }`,
		},
	}
	for _, tc := range cases {
		u := gradedSurfaceUnit(tc.language, tc.path, tc.source)
		if tc.language == "rust" {
			for _, op := range u.ops {
				if op.name == "process" {
					op.owner = "Example"
					op.fieldTypes = map[string]string{"d": "Driver"}
				}
			}
		}
		byKey := operationIndex([]unit{u})
		if !gradeGuaranteedCleanup(u.ops, []unit{u}, byKey) {
			t.Errorf("%s: cleanup was not recognized", tc.name)
		}
	}

	noisy := gradedSurfaceUnit("java", "Noisy.java", `class Driver { boolean active; void acquire() { active=true; } void log() {} } class Example { private Driver d=new Driver(); void process() { d.acquire(); try { d.log(); } finally { d.log(); } } }`)
	if gradeGuaranteedCleanup(noisy.ops, []unit{noisy}, operationIndex([]unit{noisy})) {
		t.Error("finally with only a logging call was treated as guaranteed cleanup")
	}
}

func TestGradedDeferDoesNotCaptureFollowingStatements(t *testing.T) {
	u := gradedSurfaceUnit("go", "example.go", `package sample;type Driver struct{active bool};func(d *Driver)Set(){d.active=true};type Example struct{d Driver};func(e *Example)Process(){defer println("log");e.d.Set()}`)
	index := map[string][]*operation{}
	for _, op := range u.ops {
		indexOperation(index, op)
	}
	for _, op := range u.ops {
		if op.name == "Process" && gradeGuaranteedCleanup([]*operation{op}, []unit{u}, index) {
			t.Fatal("later mutation was attributed to deferred logging")
		}
	}
}

func TestGradedUnconditionalGuardsHandlesTruncatedGuard(t *testing.T) {
	truncated := []token{{text: "if"}, {text: "("}, {text: "ready"}, {text: ")"}}
	if !gradedUnconditionalGuards(truncated, len(truncated)) {
		t.Fatal("truncated guard at end should not invalidate the scan")
	}

	unmatched := []token{{text: "if"}, {text: "("}, {text: "ready"}}
	if gradedUnconditionalGuards(unmatched, len(unmatched)) {
		t.Fatal("unmatched guard should remain invalid")
	}
}
