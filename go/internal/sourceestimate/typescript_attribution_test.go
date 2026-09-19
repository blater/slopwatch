package sourceestimate

import "testing"

func TestTypeScriptSupportingClassProof(t *testing.T) {
	for _, test := range []struct {
		source string
		want   bool
	}{
		{`class Data { private value: number; constructor(value: number) { this.value=value; } getValue() { return this.value; } }`, true},
		{`class Data { private value: number; constructor(value: number = audit()) {} getValue() { return this.value; } }`, false},
		{`class Data { private value: number; getValue() { return this.value*2; } }`, false},
		{`class Data { private value: number = audit(); getValue() { return this.value; } }`, false},
		{`class Data { private value: number; getValue() { return this.value; } update() { this.value++; } }`, false},
	} {
		tokens, limited, valid := lex([]byte(test.source))
		roles := supportingTypeScriptOwners([]unit{{file: File{Language: "typescript", Path: "service.ts"}, pkg: ".", tokens: tokens, limited: limited, lexicallyValid: valid}})
		if (len(roles) > 0) != test.want {
			t.Fatalf("source %s roles=%v want=%v", test.source, roles, test.want)
		}
	}
}

func TestTypeScriptAudienceEntryPointsAndDelegation(t *testing.T) {
	source := func(path, text string) File { return File{Path: path, Language: "typescript", Source: []byte(text)} }
	direct := AnalyzeTypeScriptFiles([]File{source("service.ts", `export function run(x:number) {return x*2;}`)})["service.ts"]
	for _, files := range [][]File{
		{source("service.ts", `function twice(x:number) {return x*2;} export function run(x:number) {return twice(x);}`)},
		{source("service.ts", `import { twice } from './helper'; export function run(x:number) {return twice(x);}`), source("helper.ts", `export function twice(x:number) {return x*2;}`)},
		{source("service.ts", `export function run(x:number) {return x*2;} class Data { value:number; getValue(){return this.value;} }`)},
		{source("service.ts", `export function run(x:number) {return x*2;}`), source("data.ts", `class Data { value:number; getValue(){return this.value;} }`)},
	} {
		got := AnalyzeTypeScriptFiles(files)["service.ts"]
		if got.Burden != direct.Burden || got.Hidden != direct.Hidden {
			t.Fatalf("direct=%+v variant=%+v", direct, got)
		}
	}
	internal := AnalyzeTypeScriptFiles([]File{source("service.ts", `function create(x:number){return x*2;} class Data { value:number; getValue(){return this.value;} }`)})["service.ts"]
	if internal.Hidden != 2 || internal.RoleOnly {
		t.Fatalf("internal factory lost: %+v", internal)
	}
	independent := AnalyzeTypeScriptFiles([]File{source("service.ts", `export function run(x:number){return x*2;} export function identity(x:number){return x;}`)})["service.ts"]
	if independent.Hidden != 0 || len(independent.Abstractions) != 2 {
		t.Fatalf("independent shallow function hidden: %+v", independent)
	}
}

func TestTypeScriptSupportingRoleDoesNotHideTopLevelEffects(t *testing.T) {
	result := AnalyzeTypeScriptFiles([]File{{Path: "service.ts", Language: "typescript", Source: []byte(`import { audit } from './audit'
audit()
class Data { value:number; getValue(){return this.value;} }`)}})["service.ts"]
	if result.RoleOnly {
		t.Fatalf("top-level execution exempted: %+v", result)
	}
}
