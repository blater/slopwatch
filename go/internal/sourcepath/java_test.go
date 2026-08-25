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
