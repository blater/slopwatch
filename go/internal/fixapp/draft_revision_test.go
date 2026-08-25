package fixapp

import (
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
)

func TestReviseDraftKeepsVerifierAndInstructionsInSync(t *testing.T) {
	draft := FixDraft{
		Revision: 3, TargetScore: 100, ChangeScope: "targets",
		Targets:      []fix.RepoPath{"a.go"},
		Preflight:    candidate.PreflightResult{AllowedPaths: []fix.RepoPath{"a.go", "a_test.go"}},
		Baseline:     baselineWithMetric("a.go", "cog", 14),
		Instructions: agent.InstructionDocument{Envelope: "caller supplied unsafe envelope"},
	}
	revised, err := ReviseDraft(draft, DraftEdits{
		TargetScore: 80, Focus: []fix.MetricGoal{{Metric: "cog", Maximum: 10}},
		ChangeScope: "targets-and-tests", ValidationPlanID: "go-test", Guidance: "Keep the API stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 4 || revised.TargetScore != 80 || revised.Baseline.Contract.Goal.MaximumScore != 80 {
		t.Fatalf("revised contract = %+v", revised)
	}
	if !strings.Contains(revised.Instructions.Objective, "SCORE <= 80") || !strings.Contains(revised.Instructions.Objective, "cog <= 10") ||
		!strings.Contains(revised.Instructions.Objective, "go-test") || revised.Instructions.UserGuidance != "Keep the API stable" {
		t.Fatalf("revised instructions = %+v", revised.Instructions)
	}
	if revised.Instructions.Envelope == "caller supplied unsafe envelope" || revised.Instructions.Envelope == "" {
		t.Fatal("revision did not restore the locked service-owned envelope")
	}
	if len(revised.AllowedPaths) != 2 || revised.AllowedPaths[1] != "a_test.go" || !strings.Contains(revised.Instructions.Objective, "a_test.go") {
		t.Fatalf("revised frozen scope = %v, prompt=%q", revised.AllowedPaths, revised.Instructions.Objective)
	}
}

func TestEffectiveBranchTemplatePrefersDeliveryWithLegacyFallback(t *testing.T) {
	tests := []struct {
		name     string
		resolved appconfig.Resolved
		want     string
	}{
		{
			name: "delivery setting wins",
			resolved: appconfig.Resolved{
				Fix:      appconfig.FixDefaults{BranchTemplate: "legacy/{target-stem}"},
				Delivery: appconfig.Delivery{BranchTemplate: "delivery/{target-stem}"},
				Origins:  map[string]appconfig.Origin{"delivery.branch_template": appconfig.OriginUser, "fix.branch_template": appconfig.OriginUser},
			},
			want: "delivery/{target-stem}",
		},
		{
			name: "empty delivery setting falls back",
			resolved: appconfig.Resolved{
				Fix: appconfig.FixDefaults{BranchTemplate: "legacy/{target-stem}"},
			},
			want: "legacy/{target-stem}",
		},
		{
			name: "explicit legacy setting survives built-in delivery default",
			resolved: appconfig.Resolved{
				Fix:      appconfig.FixDefaults{BranchTemplate: "legacy/{target-stem}"},
				Delivery: appconfig.Delivery{BranchTemplate: "built-in/{target-stem}"},
				Origins:  map[string]appconfig.Origin{"delivery.branch_template": appconfig.OriginBuiltIn, "fix.branch_template": appconfig.OriginRepository},
			},
			want: "legacy/{target-stem}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveBranchTemplate(test.resolved); got != test.want {
				t.Fatalf("effectiveBranchTemplate() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestConfiguredFocusGoalsOnlyUsesCompleteAvailableMetrics(t *testing.T) {
	targets := baselineWithMetric("a.go", "cog", 14).Contract.Targets
	targets = append(targets, fix.TargetSnapshot{Path: "b.go", Metrics: map[fix.MetricID]fix.MetricValue{
		"cog": {ID: "cog", Value: 19, Complete: true}, "npath": {ID: "npath", Value: 8, Complete: false},
	}})
	if _, err := configuredFocusGoals([]fix.MetricID{"cog", "npath", "missing"}, targets); err == nil {
		t.Fatal("incomplete configured focus metrics were silently accepted")
	}
}

func baselineWithMetric(path fix.RepoPath, id fix.MetricID, value float64) fixanalysis.BaselineSnapshot {
	return fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
		Goal: fix.ScoringGoal{MaximumScore: 100},
		Targets: []fix.TargetSnapshot{{Path: path, Score: 120, Complete: true, Metrics: map[fix.MetricID]fix.MetricValue{
			id: {ID: id, Value: value, Complete: true},
		}}},
	}}
}
