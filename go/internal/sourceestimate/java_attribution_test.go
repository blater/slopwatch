package sourceestimate

import "testing"

func TestAnalyzeJavaFilesKeepsPackageServiceAndDropsConstructorRoots(t *testing.T) {
	withConstructor := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
class Worker {
  Worker() { }
  public int run(int x) { return x * 2; }
}`)}})["Service.java"]
	withoutConstructor := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
class Worker {
  public int run(int x) { return x * 2; }
}`)}})["Service.java"]
	if !withConstructor.Applicable || withConstructor.Burden != withoutConstructor.Burden || withConstructor.Hidden != withoutConstructor.Hidden {
		t.Fatalf("internal constructor changed package service: with=%+v without=%+v", withConstructor, withoutConstructor)
	}
}

func TestAnalyzeJavaFilesKeepsBehaviorfulConstructorRoot(t *testing.T) {
	result := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
class Worker {
  Worker() { if (System.currentTimeMillis() < 0) throw new RuntimeException(); }
}`)}})["Service.java"]
	if !result.Applicable || result.Hidden == 0 {
		t.Fatalf("behaviorful internal constructor was dropped: %+v", result)
	}
}

func TestAnalyzeJavaFilesTreatsNestedPassiveErrorAsSupporting(t *testing.T) {
	withSupport := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
public class Service {
  static final class Result extends Exception {
    private final int code;
    Result(int code) { this.code = code; }
    int code() { return code; }
  }
  public int run(int x) { return x * 2; }
}`)}})["Service.java"]
	withoutSupport := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
public class Service {
  public int run(int x) { return x * 2; }
}`)}})["Service.java"]
	if !withSupport.Applicable || withSupport.Burden != withoutSupport.Burden || withSupport.Hidden != withoutSupport.Hidden {
		t.Fatalf("nested support type changed service root: with=%+v without=%+v", withSupport, withoutSupport)
	}
	roles := ClassifyJavaSources([]File{{Path: "Service.java", Language: "java", Source: []byte(`
class Result extends Exception { private final int code; Result(int code) { this.code = code; } int code() { return code; } }
`)}})["Service.java"]
	if len(roles) != 1 || roles[0].Role != roleJavaErrorData || !roles[0].Supporting {
		t.Fatalf("nested error role was not structural: %+v", roles)
	}
}

func TestAnalyzeJavaFilesDoesNotExemptComputedGetter(t *testing.T) {
	result := AnalyzeJavaFiles([]File{{Path: "Payload.java", Language: "java", Source: []byte(`
class Payload {
  private int value;
  Payload(int value) { this.value = value; }
  int value() { return value + 1; }
}`)}})["Payload.java"]
	if result.RoleOnly || !result.Applicable || result.Hidden == 0 {
		t.Fatalf("computed getter received passive exemption: %+v", result)
	}
}

func TestAnalyzeJavaFilesRejectsExecutablePassiveInitialization(t *testing.T) {
	files := []File{{Path: "Payload.java", Language: "java", Source: []byte(`
class Payload {
  private int value = compute();
  private static int compute() { return 1; }
  int value() { return value; }

}`)}}
	if roles := ClassifyJavaSources(files)["Payload.java"]; len(roles) != 0 {
		t.Fatalf("executable initializer received passive role: %+v", roles)
	}
}

func TestJavaPassiveGetterMustReturnOwnedInstanceField(t *testing.T) {
	files := []File{{Path: "Payload.java", Language: "java", Source: []byte(`
class Payload {
  private int value;
  private static int global;
  Payload(int value) { this.value = value; }
  int value() { return global; }
}`)}}
	if roles := ClassifyJavaSources(files)["Payload.java"]; len(roles) != 0 {
		t.Fatalf("getter for unrelated static state received passive role: %+v", roles)
	}
}

func TestAnalyzeJavaFilesReportsPackageOwnerAbstraction(t *testing.T) {
	result := AnalyzeJavaFiles([]File{{Path: "Service.java", Language: "java", Source: []byte(`
class Worker {
  Worker() { }
  int run(int x) { return x * 2; }
}`)}})["Service.java"]
	if len(result.Abstractions) != 1 || result.Abstractions[0].Name != "Worker" || result.Abstractions[0].Audience != "package" {
		t.Fatalf("package service abstraction missing: %+v", result.Abstractions)
	}
}

func TestJavaNestedSupportDoesNotHideStatefulOwner(t *testing.T) {
	base := `public class Service { private int state; public int run(int x) {state++;return x*2;} }`
	combined := `public class Service { private int state; public int run(int x) {state++;return x*2;} static final class ErrorData extends RuntimeException { private final int code; ErrorData(int code){this.code=code;} int code(){return code;} } }`
	_, a := AnalyzeWithAttribution([]File{{Path: "Service.java", Language: "java", Source: []byte(base)}})
	_, b := AnalyzeWithAttribution([]File{{Path: "Nested.java", Language: "java", Source: []byte(`final class Nested { static final class ErrorData extends RuntimeException {private final int code; ErrorData(int code){this.code=code;} int code(){return code;} } }`)}, {Path: "Service.java", Language: "java", Source: []byte(combined)}})
	if b["Service.java"].RoleOnly || a["Service.java"].Burden != b["Service.java"].Burden || a["Service.java"].Hidden != b["Service.java"].Hidden {
		t.Fatalf("base=%+v combined=%+v", a, b)
	}
}

func TestJavaSiblingSupportDoesNotHideStatefulOwner(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "Service.java", Language: "java", Source: []byte(`public class Service { private int state; public int run(int x){ state++; return x*2; } }
class Data { private int value; Data(int value){this.value=value;} int value(){return value;} }`)}})
	result := results["Service.java"]
	if result.RoleOnly || len(result.Abstractions) == 0 {
		t.Fatalf("service hidden: %+v", result)
	}
}
