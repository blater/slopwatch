package sourceestimate

import "testing"

func TestParserKeepsGoNewlinePackageAndResolvesHelper(t *testing.T) {
	direct := Analyze([]File{{Path: "Service.go", Language: "go", Source: []byte("package p\nfunc Run(x int) int { return x * 2 }\n")}})["Service.go"]
	delegated := Analyze([]File{
		{Path: "Service.go", Language: "go", Source: []byte("package p\nfunc Run(x int) int { return twice(x) }\n")},
		{Path: "Helper.go", Language: "go", Source: []byte("package p\nfunc twice(x int) int { return x * 2 }\n")},
	})["Service.go"]
	assertEquivalentTransform(t, direct, delegated, "Go newline helper")
}

func TestParserMeasuresTypedExpressionArrow(t *testing.T) {
	direct := Analyze([]File{{Path: "service.ts", Language: "typescript", Source: []byte("export const run = (x: number): number => x * 2;\n")}})["service.ts"]
	delegated := Analyze([]File{{Path: "service.ts", Language: "typescript", Source: []byte("export const run = (x: number): number => twice(x);\nfunction twice(x: number): number { return x * 2; }\n")}})["service.ts"]
	assertEquivalentTransform(t, direct, delegated, "TypeScript expression arrow")
}

func TestParserNormalizesTypeScriptArrowForms(t *testing.T) {
	cases := []struct {
		name, expression, block string
	}{
		{
			name:       "plain parameter",
			expression: "export const run = x => x * 2;\n",
			block:      "export const run = x => { return x * 2; };\n",
		},
		{
			name:       "typed parameter",
			expression: "export const run = (x: number): number => x * 2;\n",
			block:      "export const run = (x: number): number => { return x * 2; };\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expression := Analyze([]File{{Path: "service.ts", Language: "typescript", Source: []byte(test.expression)}})["service.ts"]
			block := Analyze([]File{{Path: "service.ts", Language: "typescript", Source: []byte(test.block)}})["service.ts"]
			if !expression.Applicable || !block.Applicable || expression.Burden != block.Burden || expression.Hidden != block.Hidden || expression.Categories["transform"] != block.Categories["transform"] {
				t.Fatalf("arrow form changed estimate: expression=%+v block=%+v", expression, block)
			}
		})
	}
}

func TestParserMeasuresGenericRustFunction(t *testing.T) {
	direct := Analyze([]File{{Path: "service.rs", Language: "rust", Source: []byte("pub fn run(x: i32) -> i32 { x * 2 }\n")}})["service.rs"]
	delegated := Analyze([]File{{Path: "service.rs", Language: "rust", Source: []byte("pub fn run(x: i32) -> i32 { twice(x) }\nfn twice<T>(x: i32) -> i32 { x * 2 }\n")}})["service.rs"]
	assertEquivalentTransform(t, direct, delegated, "Rust generic helper")
}

func assertEquivalentTransform(t *testing.T, direct, delegated Result, label string) {
	t.Helper()
	if !direct.Applicable || !delegated.Applicable || direct.Hidden != 2 || delegated.Hidden != direct.Hidden || direct.Categories["transform"] != 2 || delegated.Categories["transform"] != direct.Categories["transform"] {
		t.Fatalf("%s changed numeric estimate: direct=%+v delegated=%+v", label, direct, delegated)
	}
}
