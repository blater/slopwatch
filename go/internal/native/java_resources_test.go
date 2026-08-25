package native

import (
	"reflect"
	"testing"
)

func TestDiscoveryExcludesJavaFilesUnderConventionalResourceRoots(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "src/main/java/example/Real.java", "class Real {}\n")
	writeTestFile(t, workspace, "src/main/java/example/resources/AlsoReal.java", "class AlsoReal {}\n")
	writeTestFile(t, workspace, "src/main/resources/archetype-resources/src/main/java/Template.java", "package ${package};\n")
	writeTestFile(t, workspace, "module/src/integrationTest/resources/Fixture.java", "not Java\n")

	analyzer := &analysisEngine{workspace: workspace}
	discovered, err := discover(analyzer, []string{"."}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"src/main/java/example/Real.java",
		"src/main/java/example/resources/AlsoReal.java",
	}
	if !reflect.DeepEqual(discovered["java"], want) {
		t.Fatalf("discovered Java paths = %q, want %q", discovered["java"], want)
	}
}
