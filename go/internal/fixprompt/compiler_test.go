package fixprompt

import (
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestCompileIsDeterministicAndKeepsEnvelopeOutsideDetachedBody(t *testing.T) {
	t.Parallel()
	a, _ := fix.ParseRepoPath("z.go")
	b, _ := fix.ParseRepoPath("a.go")
	contract := fix.ScoringContract{
		Targets: []fix.TargetSnapshot{
			{Path: a, Score: 120, Metrics: map[fix.MetricID]fix.MetricValue{"cog": {ID: "cog", Value: 12, Complete: true}}},
			{Path: b, Score: 150},
		},
		Goal: fix.ScoringGoal{MaximumScore: 100, Focus: []fix.MetricGoal{{Metric: "cog", Maximum: 10}}},
	}
	first, err := Compile(Input{Contract: contract, AllowedScope: "targets-and-tests", AllowedPaths: []fix.RepoPath{a, b}, DetachedBody: "Custom body"})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Compile(Input{Contract: contract, AllowedScope: "targets-and-tests", AllowedPaths: []fix.RepoPath{a, b}, DetachedBody: "Custom body"})
	if first.EffectiveBody() != second.EffectiveBody() {
		t.Fatal("prompt compilation was not deterministic")
	}
	if !strings.Contains(first.EffectiveBody(), "Slopwatch-owned") || !strings.Contains(first.EffectiveBody(), "Custom body") {
		t.Fatalf("effective detached body lost envelope or custom body: %q", first.EffectiveBody())
	}
	if !strings.Contains(first.EffectiveBody(), "Exact admitted paths") || !strings.Contains(first.EffectiveBody(), "a.go") {
		t.Fatalf("effective detached body lost locked scope objective: %q", first.EffectiveBody())
	}
	if strings.Contains(first.EffectiveBody(), "Baseline evidence") {
		t.Fatal("detached body unexpectedly retained generated evidence")
	}
}

func TestCompileRejectsMissingTargets(t *testing.T) {
	t.Parallel()
	if _, err := Compile(Input{}); err == nil {
		t.Fatal("missing targets were accepted")
	}
}
