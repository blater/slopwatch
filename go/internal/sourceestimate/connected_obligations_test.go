package sourceestimate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectedCallerObligations(t *testing.T) {
	// Shared copying into reset-only fields is not a caller constraint.
	reset := `package p;class State{int left;int right;void reset(){left=0;right=0;}}`
	target := `package p;class State{int left;int right;void reset(){left=0;right=0;} int read(){if(left!=right)throw new IllegalStateException();return left;}}`
	caller := `package p;class Caller{void publish(State state,int input){state.left=input;state.right=input;}}`
	files := []File{{Path: "State.java", Language: "java", Source: []byte(target)}, {Path: "Caller.java", Language: "java", Source: []byte(caller)}}
	negative := append([]File(nil), files...)
	negative[0].Source = []byte(reset)
	if g := Analyze(negative)["State.java"].Grade; g.Surface.RepresentationUnits != 0 {
		t.Fatalf("shared copying invented constraint: %+v", g)
	}
	isolated := Analyze(files[:1])["State.java"].Grade
	full := Analyze(files)["State.java"].Grade
	if isolated == nil || full == nil || isolated.Surface.RepresentationUnits != 0 || full.Surface.RepresentationUnits != 4 {
		t.Fatalf("unconnected reset / proven caller: %+v / %+v", isolated, full)
	}
	for name, source := range map[string]string{"other type": strings.ReplaceAll(caller, "State state", "Other state"), "other package": strings.ReplaceAll(caller, "package p", "package other"), "dead local": `package p;class Caller{void publish(State state,int input){int left=input;int right=input;}}`} {
		files[1].Source = []byte(source)
		g := Analyze(files)["State.java"].Grade
		if g.Surface.RepresentationUnits != 0 {
			t.Fatalf("%s invented coupling: %+v", name, g)
		}
	}
	files[1].Source = []byte(caller)
	files[0].Source = []byte(strings.ReplaceAll(target, "int left;int right;", "private int left;private int right;"))
	if g := Analyze(files)["State.java"].Grade; g.Surface.RepresentationUnits != 0 {
		t.Fatalf("private representation exposed: %+v", g)
	}
	files[0].Source = []byte(target)
	files[1].Source = []byte(strings.ReplaceAll(caller, "State state", "Child state"))
	files = append(files, File{Path: "Child.java", Language: "java", Source: []byte(`package p;class Child extends State{}`)})
	if g := Analyze(files)["State.java"].Grade; g.Surface.RepresentationUnits != 4 {
		t.Fatalf("ancestry missing: %+v", g)
	}
}

func TestConnectedMapFactoryContract(t *testing.T) {
	source := `import java.util.Map;import java.util.HashMap;class Example{private final Map<Object,Map<Object,Node>> cache=new HashMap<>();public Node run(Object parent,Object key){return cache.computeIfAbsent(parent,k->new HashMap<>()).computeIfAbsent(key,k->{Node made=new Node();parent.attach(made);return made;});}}`
	grade := structuralResult("java", "Example.java", source).Grade
	if grade == nil || grade.Responsibilities["cache-consistency"] != 4 {
		t.Fatalf("connected cache missing: %+v", grade)
	}
	for name, s := range map[string]string{"custom type": source + `class Map{}`, "missing import": strings.ReplaceAll(source, "import java.util.Map;", ""), "discarded callback": strings.ReplaceAll(source, "return cache.computeIfAbsent", "Object discarded=cache.computeIfAbsent"), "returned different value": strings.ReplaceAll(source, "return made;", "return other;"), "overwritten value": strings.ReplaceAll(source, "parent.attach(made);", "made=other;parent.attach(made);")} {
		if got := structuralResult("java", "Example.java", s).Grade; got.Responsibilities["cache-consistency"] != 0 {
			t.Fatalf("%s invented cache: %+v", name, got)
		}
	}
}

func TestConnectedRestrictedRustAudience(t *testing.T) {
	source := `pub(crate) fn collect(x:i32,a:i32,b:i32,c:i32)->i32{x} #[cfg(test)]mod tests{#[test]fn check(){assert_eq!(super::collect(1,2,3,4),1);}}`
	a := structuralResult("rust", "lib.rs", source).Grade
	b := structuralResult("rust", "lib.rs", strings.Split(source, "#[cfg(test)]")[0]).Grade
	if a == nil || b == nil || a.Value() != b.Value() || a.Surface.InputUnits != b.Surface.InputUnits {
		t.Fatalf("tests erase restricted boundary: %+v / %+v", a, b)
	}
}

func TestConnectedObservedJavaDiagnostics(t *testing.T) {
	base := "../../../docs/evidence/shallow-v4/calibration-holdout"
	for _, pair := range []struct{ id, file string }{{"river-bound-access", "SqlBoundAccess.java"}, {"river-descriptor-scan-context", "SqlDescriptorScanContext.java"}, {"nql-hierarchy-row-context", "HierarchyRowContext.java"}, {"xmltoaster-sql-row-cursor", "SqlRowCursor.java"}} {
		source, err := os.ReadFile(filepath.Join(base, "snapshots", pair.id, pair.file))
		if err != nil {
			t.Fatal(err)
		}
		g := structuralResult("java", pair.file, string(source)).Grade
		if g == nil {
			t.Fatal("missing observed grade")
		}
		switch pair.id {
		case "nql-hierarchy-row-context":
			if g.Responsibilities["cache-consistency"] != 4 || g.Value() > 25 {
				t.Fatalf("nested retained cache contract lost: %+v", g)
			}
		case "xmltoaster-sql-row-cursor":
			if g.Responsibilities["resource"] != 2 || g.EstimatedHidden != 2 {
				t.Fatalf("cleanup/projection distinction lost: %+v", g)
			}
		default:
			found := false
			for _, limit := range g.MaterialLimitations {
				found = found || strings.HasPrefix(limit, "unobserved_package_caller_obligations:")
			}
			if !found {
				t.Fatalf("isolated missing callers not disclosed: %+v", g)
			}
		}
	}
}

func TestConnectedCallerBindingAndControl(t *testing.T) {
	target := `package p;class Target{int x;int y;boolean ready;int get(){return x;}void touch(){}}class Value{int first;int second;}`
	for _, body := range []string{`t.x=a.first;t.y=b.first;`, `if(false){t.x=a.first;t.y=a.first;}`, `t.x=a.first;t=new Target();t.y=a.first;`, `if(t.ready){}t.touch();t.ready=true;`, `if(t.ready){}if(false){t.touch();t.ready=true;}`} {
		files := []File{{Path: "Target.java", Language: "java", Source: []byte(target)}, {Path: "Caller.java", Language: "java", Source: []byte(`package p;class Caller{void run(Target t,Value a,Value b){` + body + `}}`)}}
		got := Analyze(files)["Target.java"].Grade
		if got.Surface.RepresentationUnits != 0 {
			t.Fatalf("disconnected %s: %+v", body, got)
		}
	}
}
func TestConnectedJavaContractResolution(t *testing.T) {
	source := `import java.util.*;class Example{private final Map<Object,Map<Object,Node>> cache=new HashMap<>();public Node run(Object parent,Object key){return cache.computeIfAbsent(parent,k->new HashMap<>()).computeIfAbsent(key,k->{Node made=new Node();return made;});}}`
	for _, s := range []string{strings.Replace(source, "class Example{", "class Example<Map extends Custom>{", 1), strings.Replace(source, "import java.util.*;", "import java.util.*;import custom.Map;", 1), strings.Replace(source, "Object key)", "Object key,Map cache)", 1)} {
		if got := structuralResult("java", "Example.java", s).Grade; got.Responsibilities["cache-consistency"] != 0 {
			t.Fatalf("shadowed library contract: %+v", got)
		}
	}
}
func TestConnectedJDBCIndependentAttempts(t *testing.T) {
	source := `import java.sql.Statement;import java.sql.ResultSet;import java.sql.SQLException;class Example{private final Statement statement;private final ResultSet result;Example(Statement statement,ResultSet result){this.statement=statement;this.result=result;}public void release(){try{statement.close();}catch(SQLException e){}try{result.close();}catch(SQLException e){}}}`
	got := structuralResult("java", "Example.java", source).Grade
	if got.Responsibilities["resource"] != 2 {
		t.Fatalf("independent typed cleanup missing: %+v", got)
	}
	for _, s := range []string{strings.ReplaceAll(source, "catch(SQLException e)", "catch(RuntimeException e)"), strings.Replace(source, "catch(SQLException e){}", "catch(SQLException e){return;}", 1), strings.ReplaceAll(source, "import java.sql.Statement;", ""), strings.ReplaceAll(source, "this.statement=statement", "this.statement=null")} {
		if got := structuralResult("java", "Example.java", s).Grade; got.Responsibilities["resource"] != 0 {
			t.Fatalf("unproven cleanup credited: %+v", got)
		}
	}
}

func TestConnectedModuleOutputBufferObligations(t *testing.T) {
	for _, tc := range []struct{ language, path, direct, helper, readonly, rebound string }{
		{"rust", "lib.rs", `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){a.push(x);b.push(x);}`, `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){write(x,a);write(x,b);}fn write(x:i32,out:&mut Vec<i32>){out.push(x);}`, `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){a.len();b.len();}`, `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){a=other;b=other;a.push(x);b.push(x);}`},
		{"go", "x.go", `package p;func Collect(x int,a,b []int){a[0]=x;b[0]=x}`, `package p;func Collect(x int,a,b []int){write(x,a);write(x,b)};func write(x int,out []int){out[0]=x}`, `package p;func Collect(x int,a,b []int){_ = a[0];_ = b[0]}`, `package p;func Collect(x int,a,b []int){a=other;b=other;a[0]=x;b[0]=x}`},
		{"typescript", "x.ts", `export function collect(x:number,a:number[],b:number[]){a.push(x);b.push(x);}`, `export function collect(x:number,a:number[],b:number[]){write(x,a);write(x,b);}function write(x:number,out:number[]){out.push(x);}`, `export function collect(x:number,a:number[],b:number[]){a.slice();b.slice();}`, `export function collect(x:number,a:number[],b:number[]){a=[];b=[];a.push(x);b.push(x);}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			a := structuralResult(tc.language, tc.path, tc.direct).Grade
			b := structuralResult(tc.language, tc.path, tc.helper).Grade
			if a.Surface.RepresentationUnits != 1 || b.Surface.RepresentationUnits != 1 {
				t.Fatalf("caller buffers lost with helper: %+v / %+v", a, b)
			}
			for _, s := range []string{tc.readonly, tc.rebound} {
				g := structuralResult(tc.language, tc.path, s).Grade
				if g.Surface.RepresentationUnits != 0 {
					t.Fatalf("noncaller output burden: %+v", g)
				}
			}
		})
	}
}
func TestConnectedProjectionEstimate(t *testing.T) {
	source := `class Example{private List items;public List result(){return items;}public void fill(){for(Item element:items.values()){element.apply(external());}}}`
	a := structuralResult("java", "Example.java", source).Grade
	if a.EstimatedHidden != 2 || a.Responsibilities["transform"] != 0 {
		t.Fatalf("unresolved projection should be explicit estimate: %+v", a)
	}
	for _, s := range []string{strings.ReplaceAll(source, "return items;", "return items.size();"), strings.ReplaceAll(source, "items.values()", "unrelated(items)"), strings.ReplaceAll(source, "element.apply(external());", "element=new Item();element.apply(external());")} {
		g := structuralResult("java", "Example.java", s).Grade
		for _, lim := range g.MaterialLimitations {
			if lim == "unresolved_owned_element_projection" {
				t.Fatalf("disconnected projection: %+v", g)
			}
		}
	}
}

func TestConnectedReviewDependencyVersions(t *testing.T) {
	target := `package p;class Target{int x;int y;int get(){return x;}}class Value{int first;}`
	for _, caller := range []string{`package p;class Caller{Target t;void run(Value a,Value b){this.t.x=a.first;this.t=new Target();this.t.y=a.first;}}`, `package p;class Caller{void run(Target t,Value a,Value b){t.x=a.first;a=b;t.y=a.first;}}`} {
		files := []File{{Path: "Target.java", Language: "java", Source: []byte(target)}, {Path: "Caller.java", Language: "java", Source: []byte(caller)}}
		if got := Analyze(files)["Target.java"].Grade; got.Surface.RepresentationUnits != 0 {
			t.Fatalf("changed storage reused: %+v", got)
		}
	}
}
func TestConnectedReviewJDBCTypeAndIdentity(t *testing.T) {
	source := `import java.sql.Statement;import java.sql.ResultSet;import java.sql.SQLException;class Example{private final Statement statement;private final ResultSet result;Example(Statement statement,ResultSet result){this.statement=statement;this.result=result;}public void release(){try{statement.close();}catch(SQLException e){}try{result.close();}catch(SQLException e){}}}`
	for _, s := range []string{strings.ReplaceAll(source, "catch(SQLException e)", "catch(RuntimeException Exception)"), strings.ReplaceAll(source, "catch(SQLException e)", "catch(custom.Exception e)"), strings.ReplaceAll(source, "this.statement=statement;", "this.statement=statement==null?null:null;")} {
		if g := structuralResult("java", "Example.java", s).Grade; g.Responsibilities["resource"] != 0 {
			t.Fatalf("contract prefix is not proof: %+v", g)
		}
	}
}
func TestConnectedMapFactoryHelper(t *testing.T) {
	direct := `import java.util.Map;import java.util.HashMap;class Example{private final Map<Object,Map<Object,Node>> cache=new HashMap<>();public Node run(Object parent,Object key){return cache.computeIfAbsent(parent,k->new HashMap<>()).computeIfAbsent(key,k->{Node made=new Node();parent.attach(made);return made;});}}`
	helper := strings.ReplaceAll(direct, `k->{Node made=new Node();parent.attach(made);return made;}`, `k->make(parent)`)
	helper = strings.TrimSuffix(helper, "}") + `private Node make(Object parent){Node made=new Node();parent.attach(made);return made;}}`
	a := structuralResult("java", "Example.java", direct).Grade
	b := structuralResult("java", "Example.java", helper).Grade
	if a.Responsibilities["cache-consistency"] != b.Responsibilities["cache-consistency"] || a.Value() != b.Value() {
		t.Fatalf("factory helper changed contract: %+v / %+v", a, b)
	}
}

func TestConnectedReviewOutputBindings(t *testing.T) {
	direct := `export function collect(x:number,a:number[],b:number[]){a.push(x);b.push(x);}`
	helper := `function write(x:number,out:number[]){out.push(x);}`
	for _, body := range []string{`a=[];b=[];write(x,a);write(x,b);`, `return;a.push(x);b.push(x);`, `const ignored=()=>{a.push(x);b.push(x);};`} {
		source := `export function collect(x:number,a:number[],b:number[]){` + body + `}` + helper
		g := structuralResult("typescript", "x.ts", source).Grade
		if g.Surface.RepresentationUnits != 0 {
			t.Fatalf("disconnected output %s: %+v", body, g)
		}
	}
	first := structuralResult("typescript", "x.ts", direct).Grade
	for _, body := range []string{`const c=a;const d=b;c.push(x);d.push(x);`, `const c=a;const d=b;write(x,c);write(x,d);`} {
		g := structuralResult("typescript", "x.ts", `export function collect(x:number,a:number[],b:number[]){`+body+`}`+helper).Grade
		if g.Surface.RepresentationUnits != first.Surface.RepresentationUnits {
			t.Fatalf("alias lost output storage: %+v / %+v", first, g)
		}
	}
}
func TestConnectedJavaSiblingTypeShadow(t *testing.T) {
	source := `package p;import java.util.*;class Example{private final Map<Object,Node> cache;public Node run(Object key){return cache.computeIfAbsent(key,k->new Node());}}`
	files := []File{{Path: "Example.java", Language: "java", Source: []byte(source)}, {Path: "Map.java", Language: "java", Source: []byte(`package p;class Map<K,V>{V computeIfAbsent(Object key,Object factory){return null;}}`)}}
	if g := Analyze(files)["Example.java"].Grade; g.Responsibilities["cache-consistency"] != 0 {
		t.Fatalf("sibling shadow gained library contract: %+v", g)
	}
}

func TestConnectedReviewOutputExecutionOrder(t *testing.T) {
	for _, tc := range []struct {
		body string
		want float64
	}{
		{`function ignored(){a.push(x);b.push(x);}`, 0},
		{`const ignored=()=>0;a.push(x);b.push(x);`, 1},
		{`a.push(x);b.push(x);a=[];b=[];`, 1},
		{`if(x){let a=[];let b=[];}a.push(x);b.push(x);`, 1},
	} {
		g := structuralResult("typescript", "x.ts", `export function collect(x:number,a:number[],b:number[]){`+tc.body+`}`).Grade
		if g.Surface.RepresentationUnits != tc.want {
			t.Fatalf("ordered output effects %s: %+v", tc.body, g)
		}
	}
}

func TestConnectedReviewTypedOutputBindings(t *testing.T) {
	for _, tc := range []struct{ language, path, prefix, suffix, shadow, alias string }{
		{"typescript", "x.ts", `export function collect(x:number,a:number[],b:number[]){`, `}`, `{let a:number[]=[];let b:number[]=[];a.push(x);b.push(x);}`, `let c:number[]=a;let d:number[]=b;c.push(x);d.push(x);`},
		{"go", "x.go", `package p;func Collect(x int,a,b []int){`, `}`, `{var a []int;var b []int;a[0]=x;b[0]=x}`, `var c []int=a;var d []int=b;c[0]=x;d[0]=x`},
		{"rust", "lib.rs", `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){`, `}`, `{let a:Vec<i32>=Vec::new();let b:Vec<i32>=Vec::new();a.push(x);b.push(x);}`, `let c: &mut Vec<i32> = a;let d: &mut Vec<i32> = b;c.push(x);d.push(x);`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			for _, test := range []struct {
				body string
				want float64
			}{{tc.shadow, 0}, {tc.alias, 1}} {
				g := structuralResult(tc.language, tc.path, tc.prefix+test.body+tc.suffix).Grade
				if g.Surface.RepresentationUnits != test.want {
					t.Fatalf("typed binding %s: %+v", test.body, g)
				}
			}
		})
	}
}

func TestConnectedCallerUnusedCallback(t *testing.T) {
	target := `package p;class Target{int x;int y;int get(){if(x!=y)throw new IllegalStateException();return x;}}`
	for _, tc := range []struct {
		body string
		want float64
	}{{`Runnable ignored=()->{t.x=value;t.y=value;};`, 0}, {`Runnable ignored=()->{};t.x=value;t.y=value;`, 4}} {
		files := []File{{Path: "Target.java", Language: "java", Source: []byte(target)}, {Path: "Caller.java", Language: "java", Source: []byte(`package p;class Caller{void run(Target t,int value){` + tc.body + `}}`)}}
		if g := Analyze(files)["Target.java"].Grade; g.Surface.RepresentationUnits != tc.want {
			t.Fatalf("callback eager effects %s: %+v", tc.body, g)
		}
	}
}

func TestConnectedRustMatchArmsAreEagerEffects(t *testing.T) {
	for _, tc := range []struct {
		body string
		want float64
	}{
		{`match x {0=>a.push(x),_=>b.push(x)}`, 1},
		{`let ignored=|v|{a.push(v);b.push(v);};`, 0},
		{`let ignored=|v|v;match x {0=>a.push(x),_=>b.push(x)}`, 1},
	} {
		source := `pub fn collect(x:i32,a:&mut Vec<i32>,b:&mut Vec<i32>){` + tc.body + `}`
		g := structuralResult("rust", "lib.rs", source).Grade
		if g.Surface.RepresentationUnits != tc.want {
			t.Fatalf("Rust arm/closure distinction %s: %+v", tc.body, g)
		}
	}
	source, err := os.ReadFile("../../../docs/evidence/shallow-v4/structural-fresh-holdout/snapshots/rust-surface-collector/surface.rs")
	if err != nil {
		t.Fatal(err)
	}
	g := structuralResult("rust", "surface.rs", string(source)).Grade
	if g.Value() < 26 || g.Value() > 64 || g.Surface.RepresentationUnits != 1 {
		t.Fatalf("observed collector output obligations regressed: %+v", g)
	}
}
