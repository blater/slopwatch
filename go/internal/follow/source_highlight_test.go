package follow

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHighlightSourceUsesEmbeddedGrammars(t *testing.T) {
	cases := []struct {
		name string
		path string
		text string
	}{
		{name: "go", path: "main.go", text: "package main\nfunc main() {}"},
		{name: "java", path: "Main.java", text: "class Main { void run() {} }"},
		{name: "rust", path: "main.rs", text: "fn main() { println!(\"hi\"); }"},
		{name: "typescript", path: "main.ts", text: "const answer: number = 42;"},
		{name: "tsx", path: "App.tsx", text: "export const App = () => <main />;"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := highlightSource(testCase.path, testCase.text)
			if !strings.Contains(got, "\x1b[") {
				t.Fatalf("highlightSource(%q) did not emit ANSI styling: %q", testCase.path, got)
			}
			plain := ansi.Strip(got)
			for _, fragment := range strings.Fields(testCase.text) {
				if !strings.Contains(plain, fragment) {
					t.Errorf("highlighted source lost source fragment %q", fragment)
				}
			}
		})
	}
}

func TestHighlightSourceFallsBackForUnsupportedFiles(t *testing.T) {
	const source = "plain text"
	if got := highlightSource("README.md", source); got != source {
		t.Fatalf("unsupported source was changed: %q", got)
	}
}
