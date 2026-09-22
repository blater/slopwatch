package sourceestimate

import "testing"

func transformationOperation(id, name, body string, params ...string) *operation {
	tokens, _, _ := lex([]byte(body))
	return &operation{id: id, name: name, owner: "", language: "go", pkg: "p", params: len(params), paramNames: params, body: tokens}
}

func transformationIndex(operations ...*operation) *operationLookup {
	index := newOperationLookup()
	for _, op := range operations {
		indexOperation(index, op)
	}
	return index
}

func TestReturnedTransformationNormalizesDirectAndHelperOutcomes(t *testing.T) {
	direct := transformationOperation("direct", "Run", "return x * 2 + 1", "x")
	helper := transformationOperation("helper", "twice", "return x * 2", "x")
	delegated := transformationOperation("delegated", "Run", "return twice(x) + 1", "x")
	key, transformed, complete := returnedTransformation(direct, nil, nil)
	delegatedKey, delegatedTransformed, delegatedComplete := returnedTransformation(delegated, nil, transformationIndex(delegated, helper))
	if !complete || !transformed || key == "" || key != delegatedKey || !delegatedTransformed || !delegatedComplete {
		t.Fatalf("direct=%q/%v/%v delegated=%q/%v/%v", key, transformed, complete, delegatedKey, delegatedTransformed, delegatedComplete)
	}
}

func TestReturnedTransformationPreservesRootsAndIgnoresUnreachableTail(t *testing.T) {
	same := transformationOperation("same", "Run", "return x + x; x + 99", "x")
	different := transformationOperation("different", "Run", "return x + y", "x", "y")
	sameKey, sameTransformed, sameComplete := returnedTransformation(same, nil, nil)
	differentKey, differentTransformed, differentComplete := returnedTransformation(different, nil, nil)
	if !sameComplete || !sameTransformed || sameKey == "" || !differentComplete || !differentTransformed || sameKey == differentKey {
		t.Fatalf("same=%q/%v/%v different=%q/%v/%v", sameKey, sameTransformed, sameComplete, differentKey, differentTransformed, differentComplete)
	}
}

func TestReturnedTransformationSupportsStraightLineAliasesAndNeutralValues(t *testing.T) {
	aliased := transformationOperation("aliased", "Run", "const factor = 2; value := x * factor; return value + 0", "x")
	identity := transformationOperation("identity", "Run", "return x + 0", "x")
	aliasedKey, aliasedTransformed, aliasedComplete := returnedTransformation(aliased, nil, nil)
	identityKey, identityTransformed, identityComplete := returnedTransformation(identity, nil, nil)
	if !aliasedComplete || !aliasedTransformed || aliasedKey == "" || !identityComplete || identityTransformed || identityKey != "" {
		t.Fatalf("aliased=%q/%v/%v identity=%q/%v/%v", aliasedKey, aliasedTransformed, aliasedComplete, identityKey, identityTransformed, identityComplete)
	}
}

func TestReturnedTransformationTreatsConstantHelperArgumentsAsNonTransforming(t *testing.T) {
	helper := transformationOperation("helper", "twice", "return x * 2", "x")
	caller := transformationOperation("caller", "Run", "return twice(2)")
	key, transformed, complete := returnedTransformation(caller, nil, transformationIndex(caller, helper))
	if !complete || transformed || key != "" {
		t.Fatalf("constant helper argument = %q/%v/%v", key, transformed, complete)
	}
}

func TestReturnedTransformationRejectsUnknownControlAndCall(t *testing.T) {
	control := transformationOperation("control", "Run", "if x > 0 { return x * 2 }; return x", "x")
	unknown := transformationOperation("unknown", "Run", "return missing(x)", "x")
	for _, op := range []*operation{control, unknown} {
		if key, transformed, complete := returnedTransformation(op, nil, nil); complete || transformed || key != "" {
			t.Fatalf("unsupported operation accepted: %s => %q/%v/%v", op.name, key, transformed, complete)
		}
	}
}

func TestReturnedTransformationKeepsIndependentReturnAfterUnresolvedSideEffect(t *testing.T) {
	cases := []struct {
		language string
		path     string
		source   string
	}{
		{"go", "service.go", `package p; func Run(x int) int { missing(); return x * 2 }`},
		{"java", "Service.java", `class Service { int run(int x) { missing(); return x * 2; } }`},
		{"typescript", "service.ts", `export function run(x:number) { missing(); return x * 2; }`},
		{"rust", "service.rs", `pub fn run(x:i32) -> i32 { missing(); return x * 2; }`},
	}
	for _, tc := range cases {
		_, results := AnalyzeWithAttribution([]File{{Path: tc.path, Language: tc.language, Source: []byte(tc.source)}})
		result, ok := results[tc.path]
		if !ok {
			t.Fatalf("%s: missing result", tc.language)
		}
		if result.Categories["transform"] != 2 || result.Categories["unknown_call"] == 0 || len(result.Limitations) == 0 {
			t.Fatalf("%s: returned transformation or side-effect uncertainty lost: %+v", tc.language, result)
		}
	}
}

func TestAnalyzeDeadArithmeticStaysIdentity(t *testing.T) {
	dead := Analyze([]File{{Path: "dead.go", Language: "go", Source: []byte(`package p; func Run(x int) int { if (false) { return x * 2 }; return x + 0 }`)}})["dead.go"]
	if dead.Categories["transform"] != 0 || dead.Hidden != 0 {
		t.Fatalf("dead arithmetic gained transformation credit: %+v", dead)
	}
	helper := transformationOperation("dead-helper", "twice", "if false { return x * 2 }; return x + 0", "x")
	caller := transformationOperation("dead-caller", "Run", "return twice(x)", "x")
	if key, transformed, complete := returnedTransformation(caller, nil, transformationIndex(caller, helper)); !complete || transformed || key != "" {
		t.Fatalf("dead helper arithmetic = %q/%v/%v", key, transformed, complete)
	}
	uncertain := transformationOperation("uncertain", "Run", "if (false && x || true) { return x * 2 }; return x + 0", "x")
	if _, _, complete := returnedTransformation(uncertain, nil, nil); complete {
		t.Fatal("compound boolean condition was pruned as always false")
	}
}

func TestAnalyzePartialHelperRetainsUncertaintyWithoutTransformCredit(t *testing.T) {
	partial := Analyze([]File{{Path: "partial.go", Language: "go", Source: []byte(`package p; func Run(x int) int { return missing(x) + 1 }`)}})["partial.go"]
	if partial.Categories["transform"] != 0 || partial.Categories["unknown_call"] == 0 || partial.Hidden == 0 {
		t.Fatalf("partial helper estimate lost uncertainty or gained X: %+v", partial)
	}
}
