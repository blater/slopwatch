package fixprompt

import (
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestCompileAppliesTheSavedMasterTemplateDeterministically(t *testing.T) {
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
	template := "Fix {targets} to {target_score}. Focus {focus_metrics}. Files: {allowed_paths}.\n{baseline_scores}\nBranch: {branch}"
	input := Input{Contract: contract, AllowedScope: "targets-and-tests", AllowedPaths: []fix.RepoPath{a, b}, Template: template, BranchName: "fix/parser"}
	first, err := Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Compile(input)
	if first.EffectiveBody() != second.EffectiveBody() {
		t.Fatal("prompt compilation was not deterministic")
	}
	for _, wanted := range []string{"a.go, z.go", "Fix a.go, z.go to 100", "cog <= 10", "SCORE 150", "fix/parser", "handle Git"} {
		if !strings.Contains(first.EffectiveBody(), wanted) {
			t.Fatalf("compiled prompt omitted %q: %q", wanted, first.EffectiveBody())
		}
	}
}

func TestCompileRejectsMissingTargets(t *testing.T) {
	t.Parallel()
	if _, err := Compile(Input{}); err == nil {
		t.Fatal("missing targets were accepted")
	}
}
