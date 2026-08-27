package sourcepath

import "testing"

func TestIsJavaResourceRecognizesConventionalResourceRootsOnly(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"src/main/resources/archetype-resources/src/main/java/Template.java", true},
		{"module/src/test/resources/fixtures/Example.java", true},
		{"module/src/integrationTest/resources/Example.java", true},
		{`module\src\main\resources\Example.java`, true},
		{"src/main/java/example/resources/Real.java", false},
		{"resources/src/main/java/Real.java", false},
		{"src/main/java/Real.java", false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := IsJavaResource(test.path); got != test.want {
				t.Fatalf("IsJavaResource(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestIsSourceFileExcludesGeneratedAndNonSourceArtifacts(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "src/main/java/example/App.java", want: true},
		{path: "src/test/java/example/AppTest.java", want: true},
		{path: "internal/service.go", want: true},
		{path: "web/component.tsx", want: true},
		{path: "target/generated-sources/example/Generated.java", want: false},
		{path: "target/classes/example/App.class", want: false},
		{path: "target/surefire-reports/AppTest.txt", want: false},
		{path: "build/generated/client.ts", want: false},
		{path: "node_modules/example/index.ts", want: false},
		{path: "src/main/resources/template.java", want: false},
		{path: "coverage/report.json", want: false},
		{path: "README.md", want: false},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := IsSourceFile(test.path); got != test.want {
				t.Fatalf("IsSourceFile(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}
