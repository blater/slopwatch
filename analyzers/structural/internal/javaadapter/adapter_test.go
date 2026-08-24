package javaadapter

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAdapterExtractsJavaFactsAndExcludesTestPackages(t *testing.T) {
	java, javaErr := exec.LookPath("java")
	javac, javacErr := exec.LookPath("javac")
	jar, jarErr := exec.LookPath("jar")
	if javaErr != nil || javacErr != nil || jarErr != nil {
		t.Skip("JDK tools are unavailable")
	}
	root := t.TempDir()
	helper := buildHelper(t, root, javac, jar)
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
	adapter := Adapter{JavaExecutable: java, HelperJar: helper}
	program, err := adapter.Analyze(root, paths, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
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
	withTests, err := adapter.Analyze(root, paths, map[string]any{"include_tests": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTests.Types) != 2 {
		t.Fatalf("include_tests returned %d types, want 2", len(withTests.Types))
	}
}

func TestAdapterCollectsMethodBodyFactsInOnePass(t *testing.T) {
	java, javaErr := exec.LookPath("java")
	javac, javacErr := exec.LookPath("javac")
	jar, jarErr := exec.LookPath("jar")
	if javaErr != nil || javacErr != nil || jarErr != nil {
		t.Skip("JDK tools are unavailable")
	}
	root := t.TempDir()
	helper := buildHelper(t, root, javac, jar)
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
	program, err := (Adapter{JavaExecutable: java, HelperJar: helper}).Analyze(
		root, []string{"src/main/java/example/BodyFacts.java"}, map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
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

func buildHelper(t *testing.T, root, javac, jar string) string {
	t.Helper()
	classes := filepath.Join(root, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join("..", "..", "adapters", "java", "src", "dev", "slopslap", "structural")
	sources := []string{
		"Facts.java", "Protocol.java", "JavaAnalyzer.java", "JavaParser.java",
		"JavaStatements.java", "JavaExpressions.java", "JavaTypeShapes.java", "Main.java",
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
