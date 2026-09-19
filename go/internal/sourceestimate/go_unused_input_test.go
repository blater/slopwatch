package sourceestimate

import "testing"

func TestGoUnusedInputFindsComputedSiblingArgument(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused, used int) int { return used }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input int) int { return target(input + 2, input) }
`)},
	})
	findings := results["helper.go"].Findings
	if len(findings) != 1 {
		t.Fatalf("expected one source-proven finding, got %+v (result=%+v)", findings, results["helper.go"])
	}
	finding := findings[0]
	if finding.Kind != "unused-input" || finding.Operation != "helper.go#target/0" || finding.Parameter != "unused" {
		t.Fatalf("unexpected finding identity: %+v", finding)
	}
	if len(finding.CallerFiles) != 1 || finding.CallerFiles[0] != "caller.go" {
		t.Fatalf("unexpected caller files: %+v", finding.CallerFiles)
	}
	if finding.UnnecessaryBurden != 0.25 {
		t.Fatalf("unexpected bounded burden: %+v", finding)
	}
}

func TestGoUnusedInputRejectsUnprovenCases(t *testing.T) {
	tests := []struct {
		name   string
		helper string
		caller string
	}{
		{
			name:   "unreachable caller cost",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { if false { return target(input + 2) }; return input }",
		},
		{
			name:   "used parameter",
			helper: "func target(input int) int { return input }",
			caller: "func Caller(input int) int { return target(input + 2) }",
		},
		{
			name:   "literal only",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { return target(2) + input }",
		},
		{
			name:   "non arithmetic argument",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { return target(read()) + input }\nfunc read() int { return 1 }",
		},
		{
			name:   "function value escape",
			helper: "func target(unused int) int { return 7 }",
			caller: "var callback = target\nfunc Caller(input int) int { return callback(input + 2) }",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, results := AnalyzeWithAttribution([]File{
				{Path: "helper.go", Language: "go", Source: []byte("package sample\n" + test.helper + "\n")},
				{Path: "caller.go", Language: "go", Source: []byte("package sample\n" + test.caller + "\n")},
			})
			if got := results["helper.go"].Findings; len(got) != 0 {
				t.Fatalf("unproven case produced findings: %+v", got)
			}
		})
	}
}

func TestGoUnusedInputAllowsNonIntegralParameterAndMultiStatementCaller(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused string) string { return "ok" }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input string) string {
		value := target(input)
		return value
}
`)},
		{Path: "broken.go", Language: "go", Source: []byte(`package sample
func broken( {
`)},
	})
	if got := results["helper.go"].Findings; len(got) != 1 {
		t.Fatalf("valid sibling proof was suppressed by unrelated incomplete source: %+v", got)
	}
}

func TestGoUnusedInputRejectsUnreachableSiblingCall(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused int) int { return 7 }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input int) int {
		if false { target(input) }
		return input
}
`)},
	})
	if got := results["helper.go"].Findings; len(got) != 0 {
		t.Fatalf("dead sibling call established a finding: %+v", got)
	}
}

func TestGoUnusedInputSourceInfersPublicFreeFunctionOnly(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "public.go", Language: "go", Source: []byte(`package sample
func PublicUsed(used int) int { return used }
func PublicUnused(used int, unused string) int { return used }
func PublicEscaped(used int, unused string) int { return used }
var callback = PublicEscaped
`)}})
	findings := results["public.go"].Findings
	if len(findings) != 1 {
		t.Fatalf("expected only the source-inferred public unused input: %+v", findings)
	}
	finding := findings[0]
	if finding.Operation != "public.go#PublicUnused/1" || finding.Parameter != "unused" {
		t.Fatalf("unexpected public finding identity: %+v", finding)
	}
	if finding.CallerFiles != nil {
		t.Fatalf("source-inferred finding should not claim caller witnesses: %+v", finding)
	}
}
