package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthPassiveRecordAndEnumAreMeasuredZero(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "WalFileHeader.java", `
package values;
public record WalFileHeader(int databaseIncarnation, long walGeneration) {}
`)
	writeSource(t, root, "ObservabilityBuildMode.java", `
package values;
enum ConsumerAccess { UNCHECKED, GUARDED }
public enum ObservabilityBuildMode {
    PRODUCTION(ConsumerAccess.UNCHECKED), DIAGNOSTIC(ConsumerAccess.GUARDED);
    private static final int COUNT = values().length;
    private final ConsumerAccess consumerAccess;
    ObservabilityBuildMode(ConsumerAccess consumerAccess) { this.consumerAccess = consumerAccess; }
    public ConsumerAccess consumerAccess() { return consumerAccess; }
    static int count() { return COUNT; }
}
`)
	program, err := adapter.Analyze(root, []string{"WalFileHeader.java", "ObservabilityBuildMode.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"values.WalFileHeader", "values.ObservabilityBuildMode"} {
		boundary := javaDepthBoundaryBySymbol(t, program, symbol)
		score := scoreForJavaBoundary(metrics.MeasureDepth(program), symbol)
		if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != 0 {
			t.Fatalf("passive value %s was not measured zero: score=%+v boundary=%#v", symbol, score, boundary)
		}
		if symbol == "values.WalFileHeader" {
			if !hasJavaDepthEvidence(boundary, "passive-value-object-v1", "proven") {
				t.Fatalf("record proof missing: %#v", boundary)
			}
		} else if !hasJavaDepthEvidence(boundary, "passive-enum-v1", "proven") {
			t.Fatalf("enum proof missing: %#v", boundary)
		}
	}
}

func TestJavaDepthPassiveValuesRejectBehavior(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []struct {
		name   string
		source string
		kind   string
		symbol string
	}{
		{"record default behavior", `package values; interface Behavior { default int calc(){return 1;} } record BadRecord(int value) implements Behavior {}`, "passive-value-object-v1", "values.BadRecord"},
		{"record unrelated call", `package values; record BadRecord(java.util.List<String> items) { public BadRecord { System.gc(); } }`, "passive-value-object-v1", "values.BadRecord"},
		{"record copy name impostor", `package values; class Copies { static <T> java.util.List<T> copyOf(java.util.List<T> items) { System.gc(); return items; } } record BadRecord(java.util.List<String> items) { public BadRecord { items = Copies.copyOf(items); } }`, "passive-value-object-v1", "values.BadRecord"},
		{"record copied argument behavior", `package values; record BadRecord(java.util.List<String> items) { public BadRecord { items = java.util.List.copyOf(items.subList(0,1)); } }`, "passive-value-object-v1", "values.BadRecord"},
		{"record validation", `package values; record BadRecord(int value) { public BadRecord { if (value < 0) throw new IllegalArgumentException(); } }`, "passive-value-object-v1", "values.BadRecord"},
		{"enum computation", `package values; enum BadEnum { A; public int code() { return 1; } }`, "passive-enum-v1", "values.BadEnum"},
		{"enum constant class body", `package values; enum BadBody { A { public String toString() { return "a"; } } }`, "passive-enum-v1", "values.BadBody"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writeSource(t, root, "Bad.java", test.source)
			program, err := adapter.Analyze(root, []string{"Bad.java"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			boundary := javaDepthBoundaryBySymbol(t, program, test.symbol)
			if hasJavaDepthEvidence(boundary, test.kind, "proven") {
				t.Fatalf("behavioral value was exempted: %#v", boundary)
			}
		})
	}
}

func TestJavaDepthExposedValueFieldsRemainDataOnly(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	for _, test := range []struct {
		body    string
		passive bool
	}{
		{"pages=0; leafDepth=-1;", true},
		{"pages++; leafDepth=-1;", false},
	} {
		writeSource(t, root, "State.java", `final class State { int pages; int leafDepth; void reset(){`+test.body+`} }`)
		program, err := adapter.Analyze(root, []string{"State.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		boundary := javaDepthBoundaryBySymbol(t, program, "State")
		if hasJavaDepthEvidence(boundary, "passive-value-object-v1", "proven") != test.passive {
			t.Fatalf("role %+v", boundary)
		}
	}
}
