package javaadapter

import (
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaDepthValueDeclarationsDoNotChangePenalty(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public record Value(int value) {}`,
		`public record Value<T>(T value) {}`,
		`public record Value() {}`,
		`public record Value(java.util.List<String> items) { public Value { items = java.util.List.copyOf(items); } }`,
		`import java.util.List; public record Value(List<String> items, int count) { public Value { items = List.copyOf(items); } }`,
		`public record Value(java.util.List<String> items) { public Value(java.util.List<String> items) { this.items = java.util.List.copyOf(items); } }`,
		`interface View { int value(); } public record Value(int value) implements View {}`,
		`public record Value<T>(T value) implements java.io.Serializable {}`,
		`public class Value<T> { private final T value; public Value(T value){this.value=value;} public T value(){return value;} }`,
		`public final class Value<T> extends Object { public final T value; public Value(T value){this.value=value;} }`,
	}
	for _, source := range cases {
		writeSource(t, root, "Value.java", source)
		program, err := adapter.Analyze(root, []string{"Value.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Value")
		if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != 0 {
			t.Fatalf("%s: %+v", source, score)
		}
	}
}
