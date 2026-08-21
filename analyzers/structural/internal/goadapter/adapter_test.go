package goadapter

import (
	"os"
	"path/filepath"
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
		if len(item.ForeignFields) != 1 || item.ForeignFields[0] != "Peer.Value" {
			t.Fatalf("foreign fields = %#v", item.ForeignFields)
		}
	}
	if !serviceFound {
		t.Fatal("Service type was not collected")
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

func TestAdapterMarksSemanticMetricsUnavailableOnTypeErrors(t *testing.T) {
	root := t.TempDir()
	source := `package sample
import "example.invalid/missing"
type Service struct { peer missing.Peer }
func (s Service) Run() { _ = s.peer }
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
		if available || reason == "" {
			t.Fatalf("%s availability = %v, %q", component, available, reason)
		}
	}
	if available, reason := program.Availability("service.go", "cognitive_complexity"); !available || reason != "" {
		t.Fatalf("syntax metric availability = %v, %q", available, reason)
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
