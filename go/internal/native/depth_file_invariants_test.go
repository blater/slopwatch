package native

import (
	"context"
	"github.com/blater/slopwatch/internal/report"
	"testing"
)

func TestGoFileDepthSiblingAndHelperInvariants(t *testing.T) {
	root := testInstallationRoot(t)
	analyze := func(files map[string]string) report.Document {
		workspace := t.TempDir()
		writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
		for path, source := range files {
			writeTestFile(t, workspace, path, source)
		}
		analyzer, err := New(workspace, root, Options{Languages: []string{"go"}})
		if err != nil {
			t.Fatal(err)
		}
		doc, err := analyzer.Analyze(context.Background(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	value := func(doc report.Document, path string) float64 {
		file, ok := fileByPath(doc.Files, path)
		if !ok {
			t.Fatal(path)
		}
		component := file.Components["module_shallowness"]
		if component.RawMaximum == nil || component.DepthScope != "file" {
			t.Fatalf("%s: %+v", path, component)
		}
		return *component.RawMaximum
	}
	for _, fixture := range []struct{ name, owner, helper string }{
		{"precise", "package sample; func Run(x int32) int32 { return x * 2 }", "package sample; func Run(x int32) int32 { return twice(x) }"},
		{"estimated receiver", "package sample; type Service struct{}; func (s Service) Run(x int32) int32 { return x * 2 }", "package sample; type Service struct{}; func (s Service) Run(x int32) int32 { return twice(x) }"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			baseline := value(analyze(map[string]string{"owner.go": fixture.owner}), "owner.go")
			sibling := value(analyze(map[string]string{"owner.go": fixture.owner, "unrelated.go": "package sample; func Other(a,b,c,d int32) int32 { return a }; type OtherType struct { n int }; func (o *OtherType) Change() { o.n++ }"}), "owner.go")
			extracted := value(analyze(map[string]string{"owner.go": fixture.helper, "helper.go": "package sample; func twice(x int32) int32 { return x*2 }"}), "owner.go")
			if baseline != sibling || baseline != extracted {
				t.Fatalf("baseline=%v sibling=%v extracted=%v", baseline, sibling, extracted)
			}
		})
	}
	combined := analyze(map[string]string{"types.go": "package sample; type Service struct{}", "methods.go": "package sample; func (s Service) First(x int32) int32 {return x*2}; func (s Service) Second(x int32) int32 {return x*3}"})
	split := analyze(map[string]string{"types.go": "package sample; type Service struct{}", "methods.go": "package sample; func (s Service) First(x int32) int32 {return x*2}", "second.go": "package sample; func (s Service) Second(x int32) int32 {return x*3}", "private.go": "package sample; func (s Service) helper(x int32) int32 {return x}"})
	want := value(combined, "methods.go")
	methodsValue := value(split, "methods.go")
	secondValue := value(split, "second.go")
	privateValue := value(split, "private.go")
	typesValue := value(split, "types.go")
	// Receiver methods implement one abstraction even when their declarations
	// span files. Only its implementation files receive that assessment.
	if methodsValue != want || secondValue != want || privateValue != want || typesValue != 0 {
		t.Fatalf("split receiver changed rating: combined=%v split methods=%v second=%v private=%v types=%v", want, methodsValue, secondValue, privateValue, typesValue)
	}
}
