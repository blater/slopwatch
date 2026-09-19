package javaadapter

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthTransportPreservesSamePackageBoundaries(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "One.java", `package sample; public final class One {
private One() {} public static int value(int x) { return x + 1; } }`)
	writeSource(t, root, "Two.java", `package sample; public final class Two {
private Two() {} public static int value(int x) { return x * 2; } }`)
	program, err := adapter.Analyze(root, []string{"One.java", "Two.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 2 {
		t.Fatalf("lost boundaries: %+v", scores)
	}
	for _, score := range scores {
		if score.State != facts.KnowledgeMeasured || score.Shallow == nil {
			t.Fatalf("lost flow while joining chunks: %+v", score)
		}
	}
}

func TestJavaDepthTransportRejectsOversizedFrameBeforeReadingBody(t *testing.T) {
	var input bytes.Buffer
	for _, value := range []uint32{1, (64 << 20) + 1} {
		if err := binary.Write(&input, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readDepth(bufio.NewReader(&input)); err == nil {
		t.Fatal("oversized depth frame accepted")
	}
}
