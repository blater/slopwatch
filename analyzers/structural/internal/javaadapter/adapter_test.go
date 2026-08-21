package javaadapter

import (
	"os"
	"os/exec"
	"path/filepath"
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
final class Service {
  private int total;
  int calculate(int value, boolean enabled) {
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

func buildHelper(t *testing.T, root, javac, jar string) string {
	t.Helper()
	classes := filepath.Join(root, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join("..", "..", "adapters", "java", "src", "dev", "slopslap", "structural")
	sources := []string{"Facts.java", "Protocol.java", "JavaAnalyzer.java", "Main.java"}
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
