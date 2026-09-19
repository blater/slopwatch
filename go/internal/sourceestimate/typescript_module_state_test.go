package sourceestimate

import (
	"os"
	"strings"
	"testing"
)

func tsModuleGroup(t *testing.T, source string) (Result, int) {
	t.Helper()
	r := AnalyzeTypeScriptFiles([]File{{Path: "module.ts", Language: "typescript", Source: []byte(source)}})["module.ts"]
	count := 0
	for _, a := range r.Abstractions {
		if strings.Contains(a.Name, "module-state:") {
			count++
		}
	}
	return r, count
}
func TestTypeScriptModuleStateBoundary(t *testing.T) {
	source := `let sequence=0;const pending=new Set<number>();export function begin(){const token=++sequence;pending.add(token);return token;}export function end(token:number){pending.delete(token);}export function query(){return pending.size>0;}export function unrelated(x:number){return x;}export class Other{run(x:number){return x;}}`
	result, n := tsModuleGroup(t, source)
	if n != 1 || len(result.Abstractions) != 3 {
		t.Fatalf("connected roots/unrelated boundaries: %+v", result.Abstractions)
	}
	for _, variant := range []string{
		strings.NewReplacer("pending", "storage", "begin", "start", "end", "finish", "sequence", "counter").Replace(source),
		strings.ReplaceAll(source, "pending.add(token);", "insert(token);") + `function insert(token:number){pending.add(token);}`,
		strings.ReplaceAll(source, "pending.add(token);", "const alias=pending;alias.add(token);"),
		strings.ReplaceAll(source, "pending.add(token);", "insert(pending,token);") + `function insert(buffer:Set<number>,token:number){buffer.add(token);}`,
	} {
		got, n := tsModuleGroup(t, variant)
		if n != 1 || got.PublishedPenalty() != result.PublishedPenalty() {
			t.Fatalf("equivalent module grouping: %+v / %+v", result, got)
		}
	}
}
func TestTypeScriptModuleStateRequiresConnectedEagerEffects(t *testing.T) {
	for _, source := range []string{
		`const pending=new Set<number>();export function begin(pending:Set<number>){pending.add(1);}export function query(){return pending.size;}`,
		`const pending=new Set<number>();export function begin(){const pending=new Set<number>();pending.add(1);}export function query(){return pending.size;}`,
		`const pending=new Set<number>();export function begin(){if(false){pending.add(1);}}export function query(){return pending.size;}`,
		`const pending=new Set<number>();export function begin(){const ignored=()=>pending.add(1);}export function query(){return pending.size;}`,
		`const pending=new Set<number>();export function begin(){return;pending.add(1);}export function query(){return pending.size;}`,
		`const pending=new Set<number>();export function begin(){let alias=pending;alias=new Set<number>();alias.add(1);}export function query(){return pending.size;}`,
		`class Set<T>{add(x:T){}get size(){return 1;}}const pending=new Set<number>();export function begin(){pending.add(1);}export function query(){return pending.size;}`,
		`import {Set} from "custom";const pending=new Set<number>();export function begin(){pending.add(1);}export function query(){return pending.size;}`,
		`const left=new Set<number>();const right=new Set<number>();export function begin(){left.add(1);}export function query(){return right.size;}`,
		`let value=0;export function begin(value:number){value++;return value;}export function query(){return value;}`,
	} {
		r, n := tsModuleGroup(t, source)
		if n != 0 {
			t.Fatalf("unconnected effects grouped %s: %+v", source, r.Abstractions)
		}
	}
}
func TestTypeScriptModuleScalarAndScopedAlias(t *testing.T) {
	for _, source := range []string{
		`let value=0;export function advance(){return ++value;}export function query(){return value;}`,
		`let data=new Set<number>();export function reset(){data=new Set<number>();}export function query(){return data.size;}`,
		`const data=new Set<number>();export function write(){const alias=data;{const alias=new Set<number>();alias.add(0);}alias.add(1);}export function query(){return data.size;}`,
		`const data=new Set<number>();export function write(){const decoy=()=>0;data.add(1);}export function query(){return data.size;}`,
	} {
		r, n := tsModuleGroup(t, source)
		if n != 1 {
			t.Fatalf("connected state missing %s: %+v", source, r.Abstractions)
		}
	}
}

func TestTypeScriptModuleHelperReadMustEscape(t *testing.T) {
	prefix := `const pending=new Set<number>();export function write(){pending.add(1);}function read(){return pending.size;}`
	for _, tc := range []struct {
		body string
		want int
	}{
		{`read();return 0;`, 0},
		{`return read();`, 1},
		{`const value=read();return value;`, 1},
		{`let value=read();value=0;return value;`, 0},
		{`return ignore(read());`, 0},
		{`let value=0;{const value=read();}return value;`, 0},
		{`let value=0;{value=read();}return value;`, 1},
		{`const value=read();{const value=0;}return value;`, 1},
	} {
		r, n := tsModuleGroup(t, prefix+`export function query(){`+tc.body+`}`)
		if n != tc.want {
			t.Fatalf("result connection %s: %+v", tc.body, r.Abstractions)
		}
	}
}

func TestTypeScriptModuleScalarDutyParity(t *testing.T) {
	for _, tc := range []struct{ module, class string }{
		{`let value=0;export function run(x:number){value++;return value;}`, `export class Example{private value=0;run(x:number){this.value++;return this.value;}}`},
		{`let value=0;export function run(x:number){value+=1;return value;}`, `export class Example{private value=0;run(x:number){this.value+=1;return this.value;}}`},
		{`let value=0;export function run(x:number){value+=x;return value;}`, `export class Example{private value=0;run(x:number){this.value+=x;return this.value;}}`},
		{`let value=0;export function run(x:number){value++;value+=x;return value;}`, `export class Example{private value=0;run(x:number){this.value++;this.value+=x;return this.value;}}`},
		{`let value=0;export function run(x:number){step();return value;}function step(){value++;}`, `export class Example{private value=0;run(x:number){this.step();return this.value;}private step(){this.value++;}}`},
	} {
		module, _ := tsModuleGroup(t, tc.module)
		class, _ := tsModuleGroup(t, tc.class)
		if module.Grade.Hidden != class.Grade.Hidden || module.Grade.Responsibilities["state"] != class.Grade.Responsibilities["state"] || module.Grade.Responsibilities["invariant-transformation"] != class.Grade.Responsibilities["invariant-transformation"] {
			t.Fatalf("storage location changes duty: module=%+v class=%+v", module.Grade, class.Grade)
		}
	}
}

func TestTypeScriptModuleRegistryBoundaryEvidence(t *testing.T) {
	source, err := os.ReadFile("../../../docs/evidence/shallow-v4/connected-high-holdout/snapshots/typescript-tracker-write-registry/run-registry.ts")
	if err != nil {
		t.Fatal(err)
	}
	result, count := tsModuleGroup(t, string(source))
	if count != 1 || len(result.Abstractions) != 1 {
		t.Fatalf("registry routes remain disconnected: %+v", result.Abstractions)
	}
	if result.Grade.Responsibilities["state"] != DefaultCalibration().State || result.Grade.Responsibilities["invariant-transformation"] != 0 {
		t.Fatalf("counter duty must survive grouping once: %+v", result.Grade)
	}
	t.Logf("registry score=%v grade=%+v", result.PublishedPenalty(), result.Grade)
}
