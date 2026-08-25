package unitplan

import "testing"

func TestJavaPlannerExcludesJavaFilesUnderResourceSourceRoots(t *testing.T) {
	root := fixture(t, map[string]string{
		"pom.xml":                         "<project/>\n",
		"src/main/java/example/Real.java": "class Real {}\n",
		"src/main/java/example/resources/AlsoReal.java":                      "class AlsoReal {}\n",
		"src/main/resources/archetype-resources/src/main/java/Template.java": "package ${package};\n",
	})
	plan, err := PlanWorkspace(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, unit := range plan.Units {
		if unit.Language == LanguageJava {
			sources = append(sources, unit.Sources...)
		}
	}
	want := []string{
		"src/main/java/example/Real.java",
		"src/main/java/example/resources/AlsoReal.java",
	}
	if len(sources) != len(want) || sources[0] != want[0] || sources[1] != want[1] {
		t.Fatalf("planned Java paths = %q, want %q", sources, want)
	}
}
