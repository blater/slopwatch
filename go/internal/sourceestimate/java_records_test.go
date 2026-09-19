package sourceestimate

import "testing"

func TestJavaCompactConstructorRetainsBehavior(t *testing.T) {
	for _, body := range []string{`items = Unknown.copyOf(items);`, `if (items == null) throw new IllegalArgumentException();`, `audit();`} {
		_, results := AnalyzeWithAttribution([]File{{Path: "Value.java", Language: "java", Source: []byte(`public record Value(java.util.List<String> items) { public Value { ` + body + ` } }`)}})
		result := results["Value.java"]
		if !result.Applicable || result.RoleOnly || result.Hidden <= 0 || len(result.Abstractions) == 0 {
			t.Fatalf("compact constructor %s lost behavior: %+v", body, result)
		}
	}
}
