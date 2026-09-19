package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthPassiveCarrierIsNotApplicableAndFingerprinted(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Point.java", `
public final class Point {
    private final int x;
    private final String label;
    private final String note = null;
    public Point(int x, String label) { this.x = x; this.label = label; }
    public int x() { return x; }
    public String label() { return this.label; }
    public String note() { return note; }
}`)
	first, err := adapter.Analyze(root, []string{"Point.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	firstScores := metrics.MeasureDepth(first)
	if len(firstScores) != 1 || firstScores[0].State != facts.KnowledgeMeasured ||
		firstScores[0].Shallow == nil || *firstScores[0].Shallow != 0 {
		t.Fatalf("passive carrier was not measured zero: %+v", firstScores)
	}
	if firstScores[0].InventoryFingerprint == "" {
		t.Fatal("passive carrier has no inventory fingerprint")
	}

	writeSource(t, root, "Point.java", `
public final class Point {
    private final int x;
    private final String label;
    private final String note = null;
    public Point(int x, String label) { this.x = x; this.label = label; }
    public int value() { return x; }
    public String label() { return this.label; }
    public String note() { return note; }
}`)
	second, err := adapter.Analyze(root, []string{"Point.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	secondScores := metrics.MeasureDepth(second)
	if len(secondScores) != 1 || secondScores[0].State != facts.KnowledgeMeasured ||
		secondScores[0].Shallow == nil || *secondScores[0].Shallow != 0 {
		t.Fatalf("renamed passive carrier was not measured zero: %+v", secondScores)
	}
	if firstScores[0].InventoryFingerprint == secondScores[0].InventoryFingerprint {
		t.Fatal("getter rename did not change inventory fingerprint")
	}
}

func TestJavaDepthPassiveCarrierRejectsImpureShapes(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Point { private final int x; public Point(int x) { this.x = x; } public int x() { return x; } public void set(int value) { } }`,
		`public final class Point { private final int[] values; public Point(int[] values) { this.values = values; } public int[] values() { return this.values; } }`,
		`public final class Point { private final int x; public Point(int x) throws Exception { this.x = x; } public int x() { return x; } }`,
		`public final class Point { private final int x; public Point(int x) { if (x < 0) throw new IllegalArgumentException(); this.x = x; } public int x() { return x; } }`,
		`public final class Point { private final int x; public Point(int x) { this.x = x; } public int x() { return x + 1; } }`,
		`public final class Point { private final int x = make(); public Point() {} public int x() { return x; } private static int make() { return 1; } }`,
	}
	for index, source := range cases {
		writeSource(t, root, "Point.java", source)
		program, err := adapter.Analyze(root, []string{"Point.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].State == facts.KnowledgeNotApplicable {
			t.Fatalf("impure carrier shape was exempted in case %d: %+v", index, scores)
		}
	}
}

func TestJavaDepthPassiveCarrierAllowsImplicitObjectConstructor(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Defaults.java", `
public final class Defaults {
    private final int value = 1;
    public int value() { return value; }
}`)
	program, err := adapter.Analyze(root, []string{"Defaults.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured ||
		scores[0].Shallow == nil || *scores[0].Shallow != 0 {
		t.Fatalf("implicit constructor carrier was not measured zero: %+v", scores)
	}
}

func TestJavaDepthPassiveCarrierAllowsNullStringAssignment(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Nullable.java", `
public final class Nullable {
    private final String value;
    public Nullable() { this.value = null; }
    public String value() { return value; }
}`)
	program, err := adapter.Analyze(root, []string{"Nullable.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured ||
		scores[0].Shallow == nil || *scores[0].Shallow != 0 {
		t.Fatalf("null String carrier was not measured zero: %+v", scores)
	}
}
