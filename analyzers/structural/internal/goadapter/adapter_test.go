package goadapter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/metrics"
)

func TestAdapterBuildsFunctionAndMultiMethodTypeFacts(t *testing.T) {
	root := t.TempDir()
	source := `package sample
type Peer struct { Value int }
type Service struct { state int; peer Peer }
func (s *Service) A(other Peer) int { return s.state + other.Value }
func (s Service) B() int { if s.state > 0 { return 1 }; return 0 }
func Run(ok bool) { callback := func() { if ok {} }; callback() }
`
	path := filepath.Join(root, "service.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := Analyze(root, []string{"service.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Functions) != 4 {
		t.Fatalf("functions = %d, want 4", len(program.Functions))
	}
	if len(program.PublicOperations) != 3 {
		t.Fatalf("public operations = %d, want 3", len(program.PublicOperations))
	}
	if len(program.Representation) != 1 || program.Representation[0].Entity != "Peer.Value" {
		t.Fatalf("representation exposure = %#v", program.Representation)
	}
	if len(program.PublicOperations[0].Parameters) == 0 || program.PublicOperations[0].Parameters[0].Name != "Peer" {
		t.Fatalf("first operation signature = %#v", program.PublicOperations[0])
	}
	var serviceFound bool
	for _, item := range program.Types {
		if item.Name != "Service" {
			continue
		}
		serviceFound = true
		if len(item.Methods) != 2 {
			t.Fatalf("methods = %d", len(item.Methods))
		}
		if len(item.MethodFields["A"]) != 1 || item.MethodFields["A"][0] != "state" {
			t.Fatalf("A fields = %#v", item.MethodFields["A"])
		}
		if len(item.Fields) != 2 || item.Fields[0].Public || item.Fields[1].Public {
			t.Fatalf("Service fields = %#v", item.Fields)
		}
		if len(item.ForeignFields) != 1 || item.ForeignFields[0] != "Peer.Value" {
			t.Fatalf("foreign fields = %#v", item.ForeignFields)
		}
	}
	if !serviceFound {
		t.Fatal("Service type was not collected")
	}
	for _, item := range program.Types {
		if item.Name == "Peer" && (len(item.Fields) != 1 || !item.Fields[0].Public) {
			t.Fatalf("Peer public fields = %#v", item.Fields)
		}
	}
}

func TestAdapterDoesNotMistakeAnotherReceiverForRecursion(t *testing.T) {
	root := t.TempDir()
	source := `package sample
type Peer struct{}
func (Peer) Run() {}
type Service struct{}
func (s Service) Run(peer Peer) { s.Run(); peer.Run() }
`
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := Analyze(root, []string{"service.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range program.Functions {
		if function.Receiver == "Service" && function.Name == "Run" {
			if got := metrics.Cognitive(function); got != 1 {
				t.Fatalf("recursive cognitive increment = %d, want 1", got)
			}
			return
		}
	}
	t.Fatal("Service.Run was not collected")
}

func TestAdapterFallsBackToSyntaxFactsOnTypeErrors(t *testing.T) {
	root := t.TempDir()
	source := `package sample
import "example.invalid/missing"
type Service struct { peer missing.Peer }
func (s Service) Run(other missing.Peer) { _ = s.peer; _ = other.Value }
`
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := Analyze(root, []string{"service.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"coupling_between_objects", "god_class"} {
		available, reason := program.Availability("service.go", component)
		if !available || reason != "" {
			t.Fatalf("%s availability = %v, %q", component, available, reason)
		}
	}
	if len(program.Types) != 1 {
		t.Fatalf("types = %d, want 1", len(program.Types))
	}
	item := program.Types[0]
	if len(item.ForeignTypes) != 1 || item.ForeignTypes[0] != "missing.Peer" {
		t.Fatalf("foreign types = %#v", item.ForeignTypes)
	}
	if len(item.ForeignFields) != 1 || item.ForeignFields[0] != "other.Value" {
		t.Fatalf("foreign fields = %#v", item.ForeignFields)
	}
	if fields := item.MethodFields["Run"]; len(fields) != 1 || fields[0] != "peer" {
		t.Fatalf("method fields = %#v", item.MethodFields)
	}
}

func TestSyntaxFieldFactsUseSingleParentAwareWalk(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", `package sample
type Service struct { state int }
func (s Service) Run(other any) { s.Run(other); _ = s.state; _ = other.Value }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[1].(*ast.FuncDecl)
	fields, foreign := methodFieldFacts(method, "s", "Service", map[string]struct{}{"state": {}}, source{})
	if len(fields) != 1 || fields[0] != "state" {
		t.Fatalf("fields = %#v", fields)
	}
	if len(foreign) != 1 || foreign[0] != "other.Value" {
		t.Fatalf("foreign fields = %#v", foreign)
	}
}

func TestModuleResolverHandlesSharedAndNestedModules(t *testing.T) {
	root := t.TempDir()
	for path, contents := range map[string]string{
		"go.mod":        "module example.test/root\n",
		"nested/go.mod": "module example.test/nested\n",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resolver := newModuleResolver(root)
	for _, test := range []struct {
		path string
		want string
	}{
		{"one/file.go", "example.test/root/one"},
		{"two/file.go", "example.test/root/two"},
		{"nested/pkg/file.go", "example.test/nested/pkg"},
		{"nested/pkg/file_test.go", "example.test/nested/pkg_test"},
	} {
		item := source{rel: test.path, file: &ast.File{Name: ast.NewIdent("sample")}}
		if strings.HasSuffix(test.path, "_test.go") {
			item.file.Name.Name = "sample_test"
		}
		if got := resolver.sourceImportPath(item); got != test.want {
			t.Fatalf("sourceImportPath(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func BenchmarkMethodFieldFacts(b *testing.B) {
	body := strings.Builder{}
	body.WriteString("package sample\ntype Service struct { state int }\nfunc (s Service) Run(other any) {\n")
	for index := 0; index < 1000; index++ {
		body.WriteString("_ = s.state; _ = other.Value\n")
	}
	body.WriteString("}\n")
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", body.String(), 0)
	if err != nil {
		b.Fatal(err)
	}
	method := file.Decls[1].(*ast.FuncDecl)
	fields := map[string]struct{}{"state": {}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		methodFieldFacts(method, "s", "Service", fields, source{})
	}
}

func BenchmarkModuleResolverSharedAncestor(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/root\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	items := make([]source, 1000)
	for index := range items {
		items[index] = source{
			rel:  fmt.Sprintf("feature/package%d/file.go", index),
			file: &ast.File{Name: ast.NewIdent("sample")},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		resolver := newModuleResolver(root)
		for _, item := range items {
			resolver.sourceImportPath(item)
		}
	}
}

func TestAdapterResolvesExactWorkspacePackagesWithoutBuilding(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":         "module example.test/project\n\ngo 1.22\n",
		"model/model.go": "package model\ntype Peer struct { Value int }\n",
		"service/service.go": `package service
import "example.test/project/model"
type Service struct { peer model.Peer }
func (s Service) Value() int { return s.peer.Value }
`,
	}
	for relative, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	program, err := Analyze(root, []string{"model/model.go", "service/service.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"coupling_between_objects", "god_class"} {
		if available, reason := program.Availability("service/service.go", component); !available {
			t.Fatalf("%s unavailable: %s", component, reason)
		}
	}
	for _, item := range program.Types {
		if item.Name == "Service" {
			if len(item.ForeignTypes) != 1 || item.ForeignTypes[0] != "example.test/project/model.Peer" {
				t.Fatalf("foreign types = %#v", item.ForeignTypes)
			}
			if len(item.MethodFields["Value"]) != 1 || item.MethodFields["Value"][0] != "peer" {
				t.Fatalf("method fields = %#v", item.MethodFields)
			}
			return
		}
	}
	t.Fatal("Service facts were not collected")
}

func TestAdapterRejectsSymlinkedSource(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.go")
	if err := os.WriteFile(target, []byte("package sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(root, []string{"linked.go"}); err == nil {
		t.Fatal("expected symlink rejection")
	}
	directory := filepath.Join(root, "real")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested.go"), []byte("package sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(root, []string{"alias/nested.go"}); err == nil {
		t.Fatal("expected symlinked parent rejection")
	}
	if _, err := Analyze(root, []string{"real/../real/nested.go"}); err == nil {
		t.Fatal("expected non-canonical path rejection")
	}
}
