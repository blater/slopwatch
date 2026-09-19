package sourceestimate

import (
	"math"
	"strings"
	"testing"
)

func TestAnalyzeCoversIncompleteTypedSourcesAndStableHelperExtraction(t *testing.T) {
	tests := []struct {
		name, language, service, helper string
	}{
		{"java", "java", `class Service { public static int run(Unknown x) { if (x == null) return 0; return Helper.twice(x); } }`, `class Helper { static int twice(Unknown x) { return x * 2; } }`},
		{"typescript", "typescript", `export function run(x: Missing): number { if (!x) return 0; return twice(x); }`, `function twice(x: Missing): number { return x * 2; }`},
		{"go", "go", `package p; func Run(x Missing) int { if x == nil { return 0 }; return twice(x) }`, `package p; func twice(x Missing) int { return x * 2 }`},
		{"rust", "rust", `pub fn run(x: Missing) -> i32 { if x == 0 { return 0; } twice(x) }`, `fn twice(x: Missing) -> i32 { x * 2 }`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Analyze(pairFiles(test.language, extension(test.language), test.service, test.helper))
			service := got["Service."+extension(test.language)]
			if !service.Applicable || service.Burden <= 0 || service.Hidden <= 0 || !finite(service.Burden) || !finite(service.Hidden) {
				t.Fatalf("service estimate = %+v", service)
			}
			if len(service.Dependencies) == 0 {
				t.Fatalf("helper dependency was not retained: %+v", service)
			}
		})
	}
}

func TestAnalyzeDistinguishesIdentityNoopAndNontrivialOutcome(t *testing.T) {
	identity := Analyze([]File{{Path: "id.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return x + 0 }`)}})["id.go"]
	if !identity.Applicable || identity.Hidden != 0 {
		t.Fatalf("identity estimate = %+v", identity)
	}
	unsupported := Analyze([]File{{Path: "u.rs", Language: "rust", Source: []byte(`pub fn run(x: Missing) -> Missing { unknown_call(x) }`)}})["u.rs"]
	if !unsupported.Applicable || unsupported.Hidden <= 0 || unsupported.Categories["unknown_call"] <= 0 {
		t.Fatalf("unsupported estimate lost uncertainty: %+v", unsupported)
	}
	voidCall := Analyze([]File{{Path: "void.java", Language: "java", Source: []byte(`class Service { public void run() { External.work(); } }`)}})["void.java"]
	if voidCall.Hidden == 0 || voidCall.Categories["unknown_call"] != 1 {
		t.Fatalf("unresolved void call was treated as responsibility-free: %+v", voidCall)
	}
}

func TestAnalyzeMarksOnlyExplicitlyEmptySourcesAsNoAbstraction(t *testing.T) {
	tests := []struct {
		name, language, path, source string
		proven                       bool
	}{
		{"empty", "typescript", "empty.ts", "", true},
		{"comment-only", "typescript", "empty.ts", "// comments only\n", true},
		{"go package and imports", "go", "empty.go", "package p; import (\"fmt\"; _ \"net/http\")", true},
		{"java package and imports", "java", "empty.java", "package p; import java.util.List; import static java.util.Collections.*;", true},
		{"go variable initializer", "go", "state.go", "package p; var value = compute()", false},
		{"unsupported package syntax", "go", "broken.go", "package", false},
		{"unterminated comment", "go", "broken.go", "/* not closed", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Analyze([]File{{Path: test.path, Language: test.language, Source: []byte(test.source)}})[test.path]
			if result.NoAbstractionProven != test.proven {
				t.Fatalf("no-abstraction proof = %v, want %v: %+v", result.NoAbstractionProven, test.proven, result)
			}
		})
	}
	left := Analyze([]File{{Path: "empty.go", Language: "go", Source: []byte("package p")}})["empty.go"]
	right := Analyze([]File{{Path: "service.go", Language: "go", Source: []byte("package p; func Run(x int) int { return x * 2 }")}})["service.go"]
	if Merge(left, right).NoAbstractionProven {
		t.Fatal("mixed empty and callable sources acquired an absence proof")
	}
}

func TestAnalyzeRecognizesQualifiedContrastingStatuses(t *testing.T) {
	result := Analyze([]File{{Path: "status.java", Language: "java", Source: []byte(`class Service { public int run(int x) { if (x < 0) return StatusCode.INVALID; return StatusCode.OK; } }`)}})["status.java"]
	if !result.Applicable || result.Categories["validation"] != 1 {
		t.Fatalf("qualified status validation was missed: %+v", result)
	}
}

func TestAnalyzeDoesNotTreatTypedStringPlusZeroAsNumericNoop(t *testing.T) {
	result := Analyze([]File{{Path: "string.java", Language: "java", Source: []byte(`class Service { public String run(String x) { return x + 0; } }`)}})["string.java"]
	if !result.Applicable || result.Categories["transform"] != 2 {
		t.Fatalf("typed string transform was treated as identity: %+v", result)
	}
}

func TestAnalyzeExposesMethodsAfterTheFirstExportedClassMethod(t *testing.T) {
	for _, body := range []string{"if (input < 0) throw new Error(); this.value = input;", "this.value = input * 2;", "this.value += input;"} {
		source := []byte("export class Payload { private value: number = 0; current(): number { return this.value; } store(input: number): void { " + body + " } reset(): void { this.value = 0; } }")
		result := Analyze([]File{{Path: "payload.ts", Language: "typescript", Source: source}})["payload.ts"]
		if result.Burden <= 1 || result.Hidden == 0 {
			t.Fatalf("exported class methods after current were omitted: %+v", result)
		}
	}
}

func TestAnalyzeDoesNotInventValidationForUnreachableOrIdenticalBranches(t *testing.T) {
	got := Analyze([]File{{Path: "guards.ts", Language: "typescript", Source: []byte(`export function unreachable(x: number): number { if (false && bump()) return 1; return x + 0 } export function same(x: number): number { if (x > 0) return 1; return 1 }`)}})
	if got["guards.ts"].Hidden != 0 {
		t.Fatalf("unreachable or identical branch gained hidden responsibility: %+v", got["guards.ts"])
	}
}

func TestAnalyzeStateRequiresReceiverReadModifyWrite(t *testing.T) {
	tests := []struct {
		name, language, path, copy, update string
	}{
		{"java", "java", "State.java", `class Service { public void set(String file) { this.file = file; } }`, `class Service { public void add(int delta) { this.n = this.n + delta; } }`},
		{"typescript", "typescript", "State.ts", `export function set(file: string): void { this.file = file; }`, `export function add(delta: number): void { this.n = this.n + delta; }`},
		{"go", "go", "state.go", `package p; func Set(s *Service, file string) { s.file = file }`, `package p; func Add(s *Service, delta int) { s.n += delta }`},
		{"rust", "rust", "state.rs", `pub fn set(&mut self, file: String) { self.file = file; }`, `pub fn add(&mut self, delta: i32) { self.n = self.n + delta; }`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copy := Analyze([]File{{Path: test.path, Language: test.language, Source: []byte(test.copy)}})[test.path]
			update := Analyze([]File{{Path: test.path, Language: test.language, Source: []byte(test.update)}})[test.path]
			if copy.Categories["state"] != 0 {
				t.Fatalf("plain member copy gained state credit: %+v", copy)
			}
			if update.Categories["state"] != 2 {
				t.Fatalf("receiver update lost state credit: %+v", update)
			}
		})
	}
}

func TestAnalyzeDirectAndExtractedTransformStayEquivalent(t *testing.T) {
	tests := []struct{ language, ext, direct, delegated, helper string }{
		{"java", "java", `class Service { public static int run(int x) { return x * 2; } }`, `class Service { public static int run(int x) { return Helper.twice(x); } }`, `class Helper { static int twice(int x) { return x * 2; } }`},
		{"typescript", "ts", `export function run(x: number): number { return x * 2; }`, `export function run(x: number): number { return twice(x); }`, `function twice(x: number): number { return x * 2; }`},
		{"go", "go", `package p; func Run(x int) int { return x * 2 }`, `package p; func Run(x int) int { return twice(x) }`, `package p; func twice(x int) int { return x * 2 }`},
		{"rust", "rs", `pub fn run(x: i32) -> i32 { x * 2 }`, `pub fn run(x: i32) -> i32 { twice(x) }`, `fn twice(x: i32) -> i32 { x * 2 }`},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			path := "Service." + test.ext
			direct := Analyze([]File{{Path: path, Language: test.language, Source: []byte(test.direct)}})[path]
			delegated := Analyze(pairFiles(test.language, test.ext, test.delegated, test.helper))[path]
			if !direct.Applicable || !delegated.Applicable || direct.Burden != delegated.Burden || direct.Hidden != delegated.Hidden || direct.Categories["transform"] != delegated.Categories["transform"] {
				t.Fatalf("extraction changed estimate: direct=%+v delegated=%+v", direct, delegated)
			}
			identity := Analyze([]File{{Path: path, Language: test.language, Source: []byte(strings.Replace(test.direct, "x * 2", "x", 1))}})[path]
			parameter := map[string]string{"java": "int x", "typescript": "x: number", "go": "x int", "rust": "x: i32"}[test.language]
			extra := parameter + ", " + strings.Replace(parameter, "x", "unused", 1)
			burden := Analyze([]File{{Path: path, Language: test.language, Source: []byte(strings.Replace(test.direct, parameter, extra, 1))}})[path]
			score := func(result Result) float64 { return 100 * result.Burden / (result.Burden + 2*result.Hidden) }
			if !(score(direct) < score(identity) && score(burden) > score(direct)) {
				t.Fatalf("responsibility/burden incentive: identity=%+v transform=%+v extra burden=%+v", identity, direct, burden)
			}
		})
	}
}

func TestAnalyzeHelperBindingIgnoresUnusedDynamicArgument(t *testing.T) {
	tests := []struct{ language, ext, direct, delegated, helper string }{
		{"java", "java", `class Service { public static int run(int x) { return 0; } }`, `class Service { public static int run(int x) { return Helper.zero(0, x); } }`, `class Helper { static int zero(int y, int ignored) { return y * 2; } }`},
		{"typescript", "ts", `export function run(x: number): number { return 0; }`, `export function run(x: number): number { return zero(0, x); }`, `function zero(y: number, ignored: number): number { return y * 2; }`},
		{"go", "go", `package p; func Run(x int) int { return 0 }`, `package p; func Run(x int) int { return zero(0, x) }`, `package p; func zero(y int, ignored int) int { return y * 2 }`},
		{"rust", "rs", `pub fn run(x: i32) -> i32 { 0 }`, `pub fn run(x: i32) -> i32 { zero(0, x) }`, `fn zero(y: i32, ignored: i32) -> i32 { y * 2 }`},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			path := "Service." + test.ext
			direct := Analyze([]File{{Path: path, Language: test.language, Source: []byte(test.direct)}})[path]
			delegated := Analyze(pairFiles(test.language, test.ext, test.delegated, test.helper))[path]
			if direct.Hidden != 0 || delegated.Hidden != direct.Hidden || delegated.Burden != direct.Burden {
				t.Fatalf("constant binding changed estimate: direct=%+v delegated=%+v", direct, delegated)
			}
		})
	}
}

func TestAnalyzeDeduplicatesEquivalentPublicComputations(t *testing.T) {
	tests := []struct{ language, ext, source string }{
		{"java", "java", `class Service { public static int a(int x) { return x * 2; } public static int b(int y) { return y * 2; } }`},
		{"typescript", "ts", `export function a(x: number): number { return x * 2; } export function b(y: number): number { return y * 2; }`},
		{"go", "go", `package p; func A(x int) int { return x * 2 }; func B(y int) int { return y * 2 }`},
		{"rust", "rs", `pub fn a(x: i32) -> i32 { x * 2 } pub fn b(y: i32) -> i32 { y * 2 }`},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			result := Analyze([]File{{Path: "Service." + test.ext, Language: test.language, Source: []byte(test.source)}})["Service."+test.ext]
			if result.Hidden != 2 {
				t.Fatalf("equivalent public computations were double counted: %+v", result)
			}
		})
	}
}

func TestAnalyzeBoundsUnresolvedCallWork(t *testing.T) {
	var body strings.Builder
	body.WriteString("export function run(x: number): number { return ")
	for i := 0; i < 100; i++ {
		if i != 0 {
			body.WriteString(" + ")
		}
		body.WriteString("missing")
		body.WriteString("(x)")
	}
	body.WriteString("; }")
	result := Analyze([]File{{Path: "bounded.ts", Language: "typescript", Source: []byte(body.String())}})["bounded.ts"]
	if !result.Applicable || result.Hidden <= 0 || !finite(result.Hidden) || len(result.Limitations) == 0 {
		t.Fatalf("unbounded unresolved work: %+v", result)
	}
}

func TestAnalyzeResolvesOwnedFieldSourceHelper(t *testing.T) {
	direct := Analyze([]File{{Path: "Service.java", Language: "java", Source: []byte(`class Service { public int run(int x) { return x * 2; } }`)}})["Service.java"]
	delegated := Analyze([]File{
		{Path: "Service.java", Language: "java", Source: []byte(`class Service { private final Decoder decoder = new Decoder(); public int run(int x) { return decoder.decode(x); } }`)},
		{Path: "Decoder.java", Language: "java", Source: []byte(`class Decoder { int decode(int x) { return x * 2; } }`)},
	})["Service.java"]
	if !direct.Applicable || !delegated.Applicable || direct.Hidden != delegated.Hidden || direct.Burden != delegated.Burden || len(delegated.Dependencies) == 0 {
		t.Fatalf("owned source helper changed estimate: direct=%+v delegated=%+v", direct, delegated)
	}
}

func TestMergeDeduplicatesResponsibilitiesAndUnknownResiduals(t *testing.T) {
	left := Analyze([]File{{Path: "a.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return x * 2 }`)}})["a.go"]
	right := Analyze([]File{{Path: "b.go", Language: "go", Source: []byte(`package p; func Run(y int) int { return y * 2 }`)}})["b.go"]
	merged := Merge(left, right)
	if merged.Burden != left.Burden+right.Burden || merged.Hidden != left.Hidden || merged.Categories["transform"] != 2 {
		t.Fatalf("equivalent responsibilities were not merged: left=%+v right=%+v merged=%+v", left, right, merged)
	}
	unknownLeft := Analyze([]File{{Path: "u1.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return missing(x) }`)}})["u1.go"]
	unknownRight := Analyze([]File{{Path: "u2.go", Language: "go", Source: []byte(`package p; func Run(y int) int { return other(y) }`)}})["u2.go"]
	if got := Merge(unknownLeft, unknownRight); got.Hidden != 1 || got.Categories["unknown_call"] != 1 {
		t.Fatalf("unknown residual was counted per call: %+v", got)
	}
}

func TestGoPrivateHelperMovementDoesNotChangePublicEstimate(t *testing.T) {
	inline := Analyze([]File{{Path: "service.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return twice(x) }; func twice(x int) int { return x * 2 }`)}})["service.go"]
	split := Analyze([]File{
		{Path: "service.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return twice(x) }`)},
		{Path: "helper.go", Language: "go", Source: []byte(`package p; func twice(x int) int { return x * 2 }`)},
	})
	moved := Merge(split["service.go"], split["helper.go"])
	if !inline.Applicable || !moved.Applicable || inline.Burden != moved.Burden || inline.Hidden != moved.Hidden || inline.Categories["transform"] != moved.Categories["transform"] {
		t.Fatalf("private helper movement changed public estimate: inline=%+v moved=%+v", inline, moved)
	}
	if split["helper.go"].Applicable {
		t.Fatalf("private helper became a public route after moving files: %+v", split["helper.go"])
	}
}

func TestAnalyzeGoFilesAttributesSplitReceiverSurface(t *testing.T) {
	files := []File{
		{Path: "contract.go", Language: "go", Source: []byte(`package p; type Service struct{}`)},
		{Path: "run.go", Language: "go", Source: []byte(`package p; func (Service) Run(x int) int { return helper(x) }`)},
		{Path: "reset.go", Language: "go", Source: []byte(`package p; func (Service) Reset(x int) int { return x * 3 }`)},
		{Path: "helper.go", Language: "go", Source: []byte(`package p; func helper(x int) int { return x * 2 }`)},
		{Path: "private_method.go", Language: "go", Source: []byte(`package p; func (Service) helper(x int) int { return x + 1 }`)},
		{Path: "sibling.go", Language: "go", Source: []byte(`package p; func local(x int) int { return x + 1 }`)},
		{Path: "free.go", Language: "go", Source: []byte(`package p; func Exported(x int) int { return x * 4 }`)},
	}
	withSibling := AnalyzeGoFiles(files)
	withoutSibling := AnalyzeGoFiles(files[:4])
	run, reset := withSibling["run.go"], withSibling["reset.go"]
	if !run.Applicable || !reset.Applicable || run.Burden != reset.Burden || run.Hidden != reset.Hidden {
		t.Fatalf("split receiver projections diverged: run=%+v reset=%+v", run, reset)
	}
	if !withSibling["helper.go"].Applicable || !withSibling["sibling.go"].Applicable {
		t.Fatalf("private-only files lost local abstraction: helper=%+v sibling=%+v", withSibling["helper.go"], withSibling["sibling.go"])
	}
	if private := withSibling["private_method.go"]; !private.Applicable || private.Burden != run.Burden || private.Hidden != run.Hidden {
		t.Fatalf("private receiver method did not inherit full type surface: private=%+v run=%+v", private, run)
	}
	if free := withSibling["free.go"]; !free.Applicable {
		t.Fatalf("exported free function lost declaring-file attribution: %+v", free)
	}
	if run.Hidden != withoutSibling["run.go"].Hidden || run.Burden != withoutSibling["run.go"].Burden {
		t.Fatalf("unrelated private sibling changed receiver projection: with=%+v without=%+v", run, withoutSibling["run.go"])
	}
}

func TestAnalyzeGoFilesProvesSupportingErrorRepresentation(t *testing.T) {
	getter := []File{{Path: "error.go", Language: "go", Source: []byte(`package p
type localError struct { message string }
func (e localError) Error() string { return e.message }
`)}}
	result := AnalyzeGoFiles(getter)["error.go"]
	if result.Applicable || !result.RoleOnly || result.Burden != 0 || result.Hidden != 0 || len(result.Roles) != 1 || result.Roles[0] != roleSupportingErrorRepresentation {
		t.Fatalf("supporting error representation was scored as behavior: %+v", result)
	}

	withEntry := []File{{Path: "error.go", Language: "go", Source: []byte(`package p
type localError struct { message string }
func (e localError) Error() string { return e.message }
func newValue(x int) int { return x * 2 }
`)}}
	withoutGetter := []File{{Path: "entry.go", Language: "go", Source: []byte(`package p
func newValue(x int) int { return x * 2 }
`)}}
	with := AnalyzeGoFiles(withEntry)["error.go"]
	without := AnalyzeGoFiles(withoutGetter)["entry.go"]
	if !with.Applicable || with.RoleOnly || len(with.Roles) != 1 || with.Roles[0] != roleSupportingErrorRepresentation || with.Burden != without.Burden || with.Hidden != without.Hidden {
		t.Fatalf("supporting getter crowded out private entrypoint: with=%+v without=%+v", with, without)
	}
}

func TestAnalyzeGoFilesRejectsErrorRepresentationWithExecutableMethod(t *testing.T) {
	files := []File{{Path: "error.go", Language: "go", Source: []byte(`package p
type localError struct { message string }
func (e localError) Error() string { return e.message }
func (e localError) Work(x int) int { return x * 2 }
`)}}
	result := AnalyzeGoFiles(files)["error.go"]
	if result.RoleOnly || !result.Applicable || len(result.Roles) != 0 || result.Hidden == 0 {
		t.Fatalf("executable receiver behavior received supporting-only proof: %+v", result)
	}
}

func TestAnalyzeGoFilesProvesPassiveDataGetterSeparately(t *testing.T) {
	plain := AnalyzeGoFiles([]File{{Path: "data.go", Language: "go", Source: []byte(`package p
type data struct { value int }
func (d data) Value() int { return d.value }
`)}})["data.go"]
	if !plain.RoleOnly || plain.Applicable || len(plain.Roles) != 1 || plain.Roles[0] != roleSupportingDataRepresentation {
		t.Fatalf("passive data getter was not isolated: %+v", plain)
	}
	computed := AnalyzeGoFiles([]File{{Path: "data.go", Language: "go", Source: []byte(`package p
type data struct { value int }
func (d data) Value() int { return d.value + 1 }
`)}})["data.go"]
	if computed.RoleOnly || !computed.Applicable || len(computed.Roles) != 0 || computed.Hidden == 0 {
		t.Fatalf("computed getter received passive proof: %+v", computed)
	}
}

func TestAnalyzeGoFilesExcludesSameFileHelpersFromInternalRoots(t *testing.T) {
	result := AnalyzeGoFiles([]File{{Path: "service.go", Language: "go", Source: []byte(`package p
func Run(x int) int { return twice(x) }
func twice(x int) int { return x * 2 }
`)}})["service.go"]
	if len(result.Abstractions) != 1 || result.Abstractions[0].Name != "Run" {
		t.Fatalf("same-file helper became an independent abstraction: %+v", result)
	}
}

func extension(language string) string {
	switch language {
	case "java":
		return "java"
	case "typescript":
		return "ts"
	case "rust":
		return "rs"
	default:
		return "go"
	}
}

func pairFiles(language, ext, service, helper string) []File {
	if language == "typescript" || language == "rust" {
		return []File{{Path: "Service." + ext, Language: language, Source: []byte(service + "\n" + helper)}}
	}
	return []File{{Path: "Service." + ext, Language: language, Source: []byte(service)}, {Path: "Helper." + ext, Language: language, Source: []byte(helper)}}
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
