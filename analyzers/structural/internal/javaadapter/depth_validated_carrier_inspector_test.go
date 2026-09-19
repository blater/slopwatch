package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthValidatedCarrierMeasuresValidationAndGetter(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/io/riverdb/tx/LockMemoryEnvelope.java"
	writeSource(t, root, path, `package io.riverdb.tx;
public final class LockMemoryEnvelope {
    private final long maximumBytes;
    public LockMemoryEnvelope(long bytes) {
        if (bytes <= 0) throw new IllegalArgumentException("invalid lock memory envelope");
        maximumBytes = bytes;
    }
    public long maximumBytes() { return maximumBytes; }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "io.riverdb.tx.LockMemoryEnvelope")
	if score == nil || score.State != facts.KnowledgeMeasured || !score.HKnown || score.H != 1 || score.Shallow == nil {
		t.Fatalf("validated carrier score = %+v", score)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "io.riverdb.tx.LockMemoryEnvelope")
	if boundary.Creation == nil || len(boundary.Creation.InitialFields) != 1 ||
		boundary.Creation.InitialFields[0].Field != "io.riverdb.tx.LockMemoryEnvelope#maximumBytes" ||
		boundary.Creation.InitialFields[0].Value != "io.riverdb.tx.LockMemoryEnvelope#<init>(long)/arg0" {
		t.Fatalf("constructor field binding lost: %#v", boundary.Creation)
	}
	if boundary.Creation == nil || boundary.Creation.DataOnly || len(boundary.Creation.PossibleFailures) == 0 ||
		len(boundary.Creation.PassiveAccessors) != 1 {
		t.Fatalf("validated carrier creation = %#v", boundary.Creation)
	}
	getterRoute := false
	for _, family := range boundary.RouteFamilies {
		for _, route := range family.Routes {
			if route.ID == "io.riverdb.tx.LockMemoryEnvelope#maximumBytes()" {
				getterRoute = true
				if len(route.RequiredSlots) != 0 {
					t.Fatalf("getter unexpectedly requires slots: %#v", route)
				}
			}
		}
	}
	if !getterRoute {
		t.Fatal("validated getter route missing")
	}
}

func TestJavaDepthValidatedCarrierRejectsUnprovenShapes(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Envelope { private int value; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = value; } public int value() { return value; } }`,
		`public final class Envelope { private final int value; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = value; } public int value() { return value; } public void set(int value) { } }`,
		`public final class Envelope { private final int value; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = value; } public int value() { return value + 1; } }`,
		`public final class Envelope { private final int value; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = parse(value); } public int value() { return value; } private static int parse(int value) { return value; } }`,
		`public class Envelope { private final int value; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = value; } public int value() { return value; } }`,
		`public final class Envelope { private final int value; private final int hidden; public Envelope(int value) { if (value < 0) throw new IllegalArgumentException(); this.value = value; this.hidden = value; } public int value() { return value; } }`,
	}
	for index, source := range cases {
		writeSource(t, root, "Envelope.java", source)
		program, err := adapter.Analyze(root, []string{"Envelope.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("unproven carrier case %d produced unexpected scores: %+v", index, scores)
		}
		if scores[0].Shallow != nil && !scores[0].Estimated {
			t.Fatalf("unproven carrier case %d became precise: %+v", index, scores)
		}
		if len(scores[0].PreciseReasons) == 0 {
			t.Fatalf("unproven carrier case %d lost uncertainty evidence: %+v", index, scores)
		}
	}
}

func TestJavaDepthValidatedCarrierDoesNotClasswideCreditUnguardedOverload(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "Envelope.java"
	writeSource(t, root, path, `
public final class Envelope {
    private final long value;
    public Envelope(long value) {
        if (value <= 0) throw new IllegalArgumentException();
        this.value = value;
    }
    public Envelope() { this.value = 1L; }
    public long value() { return value; }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Envelope")
	if score == nil || score.State != facts.KnowledgeMeasured || !score.HKnown || score.H != 0 || score.Shallow == nil {
		t.Fatalf("mixed validated/passive constructors were mis-scored: %+v", score)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "Envelope")
	if boundary.Creation == nil || len(boundary.Creation.PassiveAccessors) != 1 {
		t.Fatalf("mixed constructor creation = %#v", boundary.Creation)
	}
	var routes int
	for _, family := range boundary.RouteFamilies {
		if family.ID == "create:Envelope" {
			routes = len(family.Routes)
		}
	}
	if routes != 2 {
		t.Fatalf("constructor routes = %d, want 2", routes)
	}
}
