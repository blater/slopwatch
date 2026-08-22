package javaadapter

import (
	"os/exec"
	"testing"
)

func TestAdapterTreatsImplicitlyPublicInterfaceMethodsAsPublic(t *testing.T) {
	java, javaErr := exec.LookPath("java")
	javac, javacErr := exec.LookPath("javac")
	jar, jarErr := exec.LookPath("jar")
	if javaErr != nil || javacErr != nil || jarErr != nil {
		t.Skip("JDK tools are unavailable")
	}
	root := t.TempDir()
	helper := buildHelper(t, root, javac, jar)
	writeSource(t, root, "src/main/java/example/Participant.java", `
package example;
public interface Participant {
  StatusCode commit(long transactionId);
  long committedSequence();
}`)
	writeSource(t, root, "src/main/java/example/StatusCode.java", `
package example;
public final class StatusCode {}`)

	program, err := (Adapter{JavaExecutable: java, HelperJar: helper}).Analyze(
		root,
		[]string{"src/main/java/example/Participant.java", "src/main/java/example/StatusCode.java"},
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.PublicOperations) != 2 {
		t.Fatalf("public operations = %d, want 2: %#v", len(program.PublicOperations), program.PublicOperations)
	}
	if len(program.PublicOperations[0].Parameters) != 1 || len(program.PublicOperations[0].Results) != 1 {
		t.Fatalf("commit signature = %#v", program.PublicOperations[0])
	}
	if len(program.PublicOperations[1].Parameters) != 0 || len(program.PublicOperations[1].Results) != 1 {
		t.Fatalf("committedSequence signature = %#v", program.PublicOperations[1])
	}
}
