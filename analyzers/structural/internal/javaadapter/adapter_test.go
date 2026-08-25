package javaadapter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestAdapterExtractsJavaFactsAndExcludesTestPackages(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "src/main/java/example/Service.java", `
package example;
public class Service {
  private int total;
  public int calculate(int value, boolean enabled) {
    if (enabled && value > 0) { total += value; }
    return total;
  }
}`)
	writeSource(t, root, "src/test/java/example/test/ServiceTest.java", `
package example.test;
final class ServiceTest { void testIt() { if (true) { return; } } }`)
	paths := []string{
		"src/main/java/example/Service.java",
		"src/test/java/example/test/ServiceTest.java",
	}
	program, err := adapter.Analyze(root, paths, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	assertBasicJavaFacts(t, program)
	withTests, err := adapter.Analyze(root, paths, map[string]any{"include_tests": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTests.Types) != 2 {
		t.Fatalf("include_tests returned %d types, want 2", len(withTests.Types))
	}
}

func javaTestAdapter(t *testing.T) (string, Adapter) {
	t.Helper()
	java, javaErr := exec.LookPath("java")
	javac, javacErr := exec.LookPath("javac")
	jar, jarErr := exec.LookPath("jar")
	if javaErr != nil || javacErr != nil || jarErr != nil {
		t.Skip("JDK tools are unavailable")
	}
	root := t.TempDir()
	return root, Adapter{JavaExecutable: java, HelperJar: buildHelper(t, root, javac, jar)}
}

func assertBasicJavaFacts(t *testing.T, program *facts.Program) {
	t.Helper()
	if len(program.Functions) != 1 || program.Functions[0].Name != "calculate" {
		t.Fatalf("unexpected functions: %#v", program.Functions)
	}
	if len(program.Types) != 1 || program.Types[0].Name != "Service" {
		t.Fatalf("unexpected types: %#v", program.Types)
	}
	if len(program.PublicOperations) != 1 || len(program.PublicOperations[0].Parameters) != 2 || len(program.PublicOperations[0].Results) != 1 {
		t.Fatalf("unexpected public operations: %#v", program.PublicOperations)
	}
	if got := program.Types[0].MethodFields["calculate#0"]; len(got) != 1 || got[0] != "total" {
		t.Fatalf("unexpected method fields: %#v", program.Types[0].MethodFields)
	}
}

func TestAdapterCollectsMethodBodyFactsInOnePass(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "src/main/java/example/BodyFacts.java", `
package example;
final class BodyFacts {
  private int total;
  private Object payload;
  public int calculate(Repository repository) {
    java.util.List<String> names = new java.util.ArrayList<>();
    Object local = (External) payload;
    if (local instanceof Marker marker) {
      repository.state.value++;
      total++;
    }
    Runnable callback = new Runnable() {
      public void run() { NestedType nested = null; total++; }
    };
    return total;
  }
}`)
	program, err := adapter.Analyze(
		root, []string{"src/main/java/example/BodyFacts.java"}, map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertMethodBodyFacts(t, program)
}

func assertMethodBodyFacts(t *testing.T, program *facts.Program) {
	t.Helper()
	if len(program.Types) != 1 {
		t.Fatalf("types = %d, want 1", len(program.Types))
	}
	typeFact := program.Types[0]
	if got := typeFact.MethodFields["calculate#0"]; !slices.Equal(got, []string{"payload", "total"}) {
		t.Fatalf("method fields = %#v, want payload and total", got)
	}
	for _, want := range []string{"External", "Marker", "Repository", "java.util.ArrayList", "java.util.List"} {
		if !slices.Contains(typeFact.ForeignTypes, want) {
			t.Errorf("foreign types %q do not contain %q", typeFact.ForeignTypes, want)
		}
	}
	if slices.Contains(typeFact.ForeignTypes, "NestedType") {
		t.Errorf("anonymous-class implementation type leaked into owner facts: %q", typeFact.ForeignTypes)
	}
	for _, want := range []string{"repository.state", "repository.state.value"} {
		if !slices.Contains(typeFact.ForeignFields, want) {
			t.Errorf("foreign fields %q do not contain %q", typeFact.ForeignFields, want)
		}
	}
}

func TestAdapterPreservesNestedLocalAndAnonymousClassDiscovery(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "src/main/java/example/NestedFacts.java", `
package example;
final class NestedFacts {
  public int outer;
  class Member { public int member; }
  Object field = new Object() { class InField { public int inField; } };
  { class InInitializer { public int initialized; } }
  void inspect() {
    class Local { int read() { return outer; } class Deep { public int deep; } }
    Object value = new Object() { class InAnonymous { public int anonymous; } };
    Runnable callback = () -> { class InLambda { public int lambda; } };
  }
}`)
	program, err := adapter.Analyze(
		root, []string{"src/main/java/example/NestedFacts.java"}, map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertNestedJavaFacts(t, program)
}

func assertNestedJavaFacts(t *testing.T, program *facts.Program) {
	t.Helper()
	var typeNames []string
	for _, item := range program.Types {
		typeNames = append(typeNames, item.Name)
	}
	wantTypes := []string{
		"NestedFacts", "Member", "InField", "InInitializer", "Local", "Deep",
		"InAnonymous", "InLambda",
	}
	if !slices.Equal(typeNames, wantTypes) {
		t.Fatalf("types = %q, want %q", typeNames, wantTypes)
	}
	if got := program.Types[0].MethodFields["inspect#0"]; !slices.Equal(got, []string{"outer"}) {
		t.Fatalf("outer method fields = %q, want nested-body outer reference", got)
	}
	var exposures []string
	for _, exposure := range program.Representation {
		exposures = append(exposures, exposure.Entity)
	}
	wantExposures := []string{
		"NestedFacts.outer", "Member.member", "InField.inField",
		"InInitializer.initialized", "Deep.deep", "InAnonymous.anonymous", "InLambda.lambda",
	}
	if !slices.Equal(exposures, wantExposures) {
		t.Fatalf("representation = %q, want %q", exposures, wantExposures)
	}
}

func TestJavaCommandUsesLowOverheadRuntimeSettings(t *testing.T) {
	got := javaCommand("java-bin", "helper.jar").Args
	want := []string{
		"java-bin", "-XX:TieredStopAtLevel=1", "-XX:+UseSerialGC", "-jar", "helper.jar",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Java helper command = %q, want %q", got, want)
	}
}

func TestProtocolAcceptsValuesBeyondLegacyLimits(t *testing.T) {
	if err := writeCount(io.Discard, 1_000_001); err != nil {
		t.Fatalf("write count beyond former limit: %v", err)
	}
	var encodedCount bytes.Buffer
	if err := binary.Write(&encodedCount, binary.BigEndian, uint32(1_000_001)); err != nil {
		t.Fatal(err)
	}
	if got, err := readCount(&encodedCount); err != nil || got != 1_000_001 {
		t.Fatalf("read count = %d, %v", got, err)
	}
	large := strings.Repeat("x", 16*1024*1024+1)
	if err := writeString(io.Discard, large); err != nil {
		t.Fatalf("write string beyond former limit: %v", err)
	}
}

func TestAnalyzePathsLeavesSmallInventoriesOnOneHelper(t *testing.T) {
	paths := make([]string, parallelMinPaths-1)
	for index := range paths {
		paths[index] = fmt.Sprintf("src/P%03d.java", index)
	}
	want := &facts.Program{Files: []string{"unchanged"}}
	calls := 0
	got, err := analyzePaths(paths, func(batch []string) (*facts.Program, error) {
		calls++
		if !slices.Equal(batch, paths) {
			t.Fatalf("helper paths changed: got %d paths, want %d", len(batch), len(paths))
		}
		return want, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("helper calls = %d, want 1", calls)
	}
	if got != want {
		t.Fatal("single-helper program was copied or merged")
	}
}

func TestAnalyzePathsMergesParallelBatchesExactlyAndInHelperOrder(t *testing.T) {
	paths := make([]string, parallelMinPaths+3)
	for index := range paths {
		paths[index] = fmt.Sprintf("src/P%03d.java", len(paths)-index-1)
	}

	var lock sync.Mutex
	batches := make([][]string, 0, javaBatchCount(len(paths)))
	got, err := analyzePaths(paths, func(batch []string) (*facts.Program, error) {
		lock.Lock()
		batches = append(batches, slices.Clone(batch))
		lock.Unlock()
		return orderedTestProgram(batch), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := orderedTestProgram(paths)
	assertMergedJavaProgram(t, got, want)
	assertExactJavaBatches(t, paths, batches)
}

func assertMergedJavaProgram(t *testing.T, got, want *facts.Program) {
	t.Helper()
	if err := got.LinkTypeMethods(); err != nil {
		t.Fatal(err)
	}
	if err := want.LinkTypeMethods(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("parallel helper facts differ from one-helper facts")
	}
}

func assertExactJavaBatches(t *testing.T, paths []string, batches [][]string) {
	t.Helper()
	if want := javaBatchCount(len(paths)); len(batches) != want || want < 2 {
		t.Fatalf("helper batches = %d, want %d (at least 2)", len(batches), want)
	}
	seen := make([]string, 0, len(paths))
	for _, batch := range batches {
		seen = append(seen, batch...)
	}
	sort.Strings(seen)
	wantSeen := slices.Clone(paths)
	sort.Strings(wantSeen)
	if !slices.Equal(seen, wantSeen) {
		t.Fatal("parallel batches did not preserve the exact source inventory")
	}
}

func TestAnalyzePathsReturnsFirstBatchFailureRegardlessOfCompletionOrder(t *testing.T) {
	pathCount := max(parallelMinPaths, parallelBatchPathGoal+1)
	paths := make([]string, pathCount)
	for index := range paths {
		paths[index] = fmt.Sprintf("src/P%03d.java", index)
	}
	firstErr := errors.New("first batch failed")
	secondErr := errors.New("second batch failed")
	secondFinished := make(chan struct{})
	_, err := analyzePaths(paths, func(batch []string) (*facts.Program, error) {
		if batch[0] == paths[0] {
			<-secondFinished
			return nil, firstErr
		}
		close(secondFinished)
		return nil, secondErr
	})
	if !errors.Is(err, firstErr) {
		t.Fatalf("error = %v, want first source batch failure", err)
	}
}

func TestMergeProgramsUsesJavaUTF16PathOrdering(t *testing.T) {
	supplementary := "src/\U00010000.java"
	privateUse := "src/\uE000.java"
	program := mergePrograms([]*facts.Program{
		{Files: []string{privateUse}, Functions: []*facts.Function{{Location: facts.Location{Path: privateUse}}}},
		{Files: []string{supplementary}, Functions: []*facts.Function{{Location: facts.Location{Path: supplementary}}}},
	})
	if !slices.Equal(program.Files, []string{supplementary, privateUse}) {
		t.Fatalf("files = %q, want Java String.compareTo order", program.Files)
	}
	if got := program.Functions[0].Location.Path; got != supplementary {
		t.Fatalf("first function path = %q, want %q", got, supplementary)
	}
}

func orderedTestProgram(paths []string) *facts.Program {
	program := &facts.Program{Unavailable: make(map[string]map[string]string)}
	for _, path := range paths {
		location := facts.Location{Path: path, Line: 1, Column: 1, EndLine: 1, EndColumn: 2}
		program.Functions = append(program.Functions, &facts.Function{Name: path, Location: location})
		program.Types = append(program.Types, &facts.Type{
			Name: path, Location: location, MethodLocations: []facts.Location{location},
		})
		program.PublicOperations = append(program.PublicOperations, &facts.PublicOperation{
			Name: path, Location: location,
		})
		program.Representation = append(program.Representation, &facts.RepresentationExposure{
			StableID: path, Location: location,
		})
		program.Files = append(program.Files, path)
	}
	sort.SliceStable(program.Functions, func(left, right int) bool {
		return program.Functions[left].Location.Path < program.Functions[right].Location.Path
	})
	sort.SliceStable(program.Types, func(left, right int) bool {
		return program.Types[left].Location.Path < program.Types[right].Location.Path
	})
	sort.SliceStable(program.PublicOperations, func(left, right int) bool {
		return program.PublicOperations[left].Location.Path < program.PublicOperations[right].Location.Path
	})
	sort.Strings(program.Files)
	return program
}

func buildHelper(t *testing.T, root, javac, jar string) string {
	t.Helper()
	classes := filepath.Join(root, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join("..", "..", "adapters", "java", "src", "dev", "slopslap", "structural")
	sources := []string{
		"Facts.java", "Protocol.java", "JavaAnalyzer.java", "JavaParser.java",
		"JavaClassFacts.java", "JavaControlStatements.java", "JavaMethodBodyFacts.java",
		"JavaNestedClasses.java", "JavaStatements.java", "JavaExpressions.java",
		"JavaTypeNames.java", "JavaTypeShapes.java", "Main.java",
	}
	arguments := []string{"--release", "17", "-d", classes}
	for _, source := range sources {
		arguments = append(arguments, filepath.Join(sourceRoot, source))
	}
	if output, err := exec.Command(javac, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("javac failed: %v: %s", err, output)
	}
	helper := filepath.Join(root, helperName)
	if output, err := exec.Command(
		jar, "--create", "--file", helper, "--main-class", "dev.slopslap.structural.Main",
		"-C", classes, ".",
	).CombinedOutput(); err != nil {
		t.Fatalf("jar failed: %v: %s", err, output)
	}
	return helper
}

func writeSource(t *testing.T, root, relative, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}
