package native

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestSourceAudienceJavaInvariants(t *testing.T) {
	root := testInstallationRoot(t)
	if _, err := os.Stat(analyzerExecutable(root, "java")); err != nil {
		t.Skip("structural Java analyzer is not built")
	}
	analyze := func(files map[string]string) report.Document {
		workspace := t.TempDir()
		for path, source := range files {
			writeTestFile(t, workspace, path, source)
		}
		analyzer, err := New(workspace, root, Options{Targets: []string{"."}, Languages: []string{"java"}, ShallowProfile: ShallowProfileResponsibilityV4})
		if err != nil {
			t.Fatal(err)
		}
		document, err := analyzer.Analyze(context.Background(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	service := `public class Service {
  private int state;
  public int run(int x) { state++; return x * 2; }
}`
	withNestedSupport := analyze(map[string]string{
		"Service.java": strings.TrimSuffix(service, "}") + `
  static final class ErrorData extends RuntimeException {
    private final int code;
    ErrorData(int code) { this.code = code; }
    int code() { return code; }
  }
}`,
		"Nested.java": `final class Nested {
  static final class ErrorData extends RuntimeException {
    private final int code;
    ErrorData(int code) { this.code = code; }
    int code() { return code; }
  }
}`,
	})
	withoutNestedSupport := analyze(map[string]string{"Service.java": service})
	assertSourceAudienceSameDepth(t, withoutNestedSupport, withNestedSupport, "Service.java", "nested support type")

	direct := analyze(map[string]string{"Service.java": `public class Service {
  private int state;
  public int run(int x) { state++; return x * 2; }
}`})
	extracted := analyze(map[string]string{
		"Service.java": `public class Service {
  private int state;
  public int run(int x) { state++; return Helpers.twice(x); }
}`,
		"Helpers.java": `final class Helpers { static int twice(int x) { return x * 2; } }`,
	})
	assertSourceAudienceSameDepth(t, direct, extracted, "Service.java", "private helper extraction")

	withSibling := analyze(map[string]string{
		"Service.java": service,
		"Other.java":   `public class Other { public int unrelated(int x) { return x + 1; } }`,
	})
	assertSourceAudienceSameDepth(t, direct, withSibling, "Service.java", "unrelated sibling")

	identity := analyze(map[string]string{
		"First.java":  `public class First { public int run(int x) { return x * 2; } }`,
		"Second.java": `public class Second { public int run(int x) { return x; } }`,
	})
	for _, path := range []string{"First.java", "Second.java"} {
		file, ok := fileByPath(identity.Files, path)
		if !ok || file.Components["module_shallowness"].DepthScope != "file" {
			t.Fatalf("independent Java identity lost for %s: %+v", path, file)
		}
	}
}

func TestSourceAudienceTypeScriptInvariants(t *testing.T) {
	root := testInstallationRoot(t)
	if _, err := os.Stat(analyzerExecutable(root, "typescript")); err != nil {
		t.Skip("structural TypeScript analyzer is not built")
	}
	analyze := func(files map[string]string) report.Document {
		workspace := t.TempDir()
		writeTestFile(t, workspace, "tsconfig.json", `{"files":[],"references":[{"path":"./src"}]}`)
		writeTestFile(t, workspace, "src/tsconfig.json", `{"compilerOptions":{"composite":true},"include":["*.ts"]}`)
		for path, source := range files {
			writeTestFile(t, workspace, path, source)
		}
		analyzer, err := New(workspace, root, Options{Targets: []string{"."}, Languages: []string{"typescript"}, TypeScriptTypes: true, ShallowProfile: ShallowProfileResponsibilityV4})
		if err != nil {
			t.Fatal(err)
		}
		document, err := analyzer.Analyze(context.Background(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	service := `export function run(x: number): number { return x * 2; }`
	withNestedSupport := analyze(map[string]string{
		"src/service.ts": service + ` class Data { private code:number; constructor(code:number){this.code=code;} getCode():number{return this.code;} }`,
		"src/nested.ts":  `class ErrorData { private code: number; constructor(code: number) { this.code = code; } getCode(): number { return this.code; } }`,
	})
	withoutNestedSupport := analyze(map[string]string{"src/service.ts": service})
	assertSourceAudienceSameDepth(t, withoutNestedSupport, withNestedSupport, "src/service.ts", "nested TypeScript support type")

	withSibling := analyze(map[string]string{
		"src/service.ts": service,
		"src/other.ts":   `export function unrelated(x: number): number { return x + 1; }`,
	})
	assertSourceAudienceSameDepth(t, withoutNestedSupport, withSibling, "src/service.ts", "unrelated TypeScript sibling")

	identity := analyze(map[string]string{
		"src/first.ts":  `export function first(x: number): number { return x * 2; }`,
		"src/second.ts": `export function second(x: number): number { return x; }`,
	})
	for _, path := range []string{"src/first.ts", "src/second.ts"} {
		file, ok := fileByPath(identity.Files, path)
		if !ok || file.Components["module_shallowness"].DepthScope != "file" {
			t.Fatalf("independent TypeScript identity lost for %s: %+v", path, file)
		}
	}
}

func assertSourceAudienceSameDepth(t *testing.T, before, after report.Document, path, caseName string) {
	t.Helper()
	left, ok := fileByPath(before.Files, path)
	if !ok {
		t.Fatalf("%s: baseline file %s missing", caseName, path)
	}
	right, ok := fileByPath(after.Files, path)
	if !ok {
		t.Fatalf("%s: comparison file %s missing", caseName, path)
	}
	leftDepth := left.Components["module_shallowness"]
	rightDepth := right.Components["module_shallowness"]
	if leftDepth.RawMaximum == nil || rightDepth.RawMaximum == nil || *leftDepth.RawMaximum != *rightDepth.RawMaximum {
		t.Fatalf("%s changed %s depth: baseline=%+v comparison=%+v", caseName, path, leftDepth, rightDepth)
	}
}
