package javaadapter

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJavaRoleIndexesPreserveReferencesAndBoundSearchWork(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	paths, err := filepath.Glob("testdata/role-index/*.java")
	if err != nil || len(paths) != 3 {
		t.Fatalf("role regression sources: %v, %v", paths, err)
	}
	args := append([]string{"-cp", adapter.HelperJar, "-d", root}, paths...)
	if output, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile role regression: %v\n%s", err, output)
	}
	classpath := strings.Join([]string{root, adapter.HelperJar}, string(filepath.ListSeparator))
	if output, err := exec.Command(adapter.JavaExecutable, "-cp", classpath, "dev.slopslap.structural.RoleIndexProbe").CombinedOutput(); err != nil {
		t.Fatalf("role regression: %v\n%s", err, output)
	}
}
