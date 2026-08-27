package fixprompt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/blater/slopmochi/internal/fix"
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
	if !strings.HasPrefix(first.EffectiveBody(), GuardrailEnvelope+"\n\n"+"Fix a.go, z.go to 100") {
		t.Fatalf("trusted guardrail was not the prompt prefix: %q", first.EffectiveBody())
	}
	for _, wanted := range []string{"a.go, z.go", "Fix a.go, z.go to 100", "cog <= 10", "SCORE 150", "fix/parser"} {
		if !strings.Contains(first.EffectiveBody(), wanted) {
			t.Fatalf("compiled prompt omitted %q: %q", wanted, first.EffectiveBody())
		}
	}
	for _, hidden := range []string{"handle Git", "measurement targets, not a write allowlist", "Required target checklist:"} {
		if strings.Contains(first.EffectiveBody(), hidden) {
			t.Fatalf("compiler added instruction text outside the configured template: %q", first.EffectiveBody())
		}
	}
}

func TestDefaultTemplateContainsTheCompleteAgentInstructions(t *testing.T) {
	t.Parallel()
	document, err := Compile(Input{
		Contract: fix.ScoringContract{
			Targets: []fix.TargetSnapshot{{Path: "one.go", Score: 90}, {Path: "two.go", Score: 80}},
			Goal:    fix.ScoringGoal{MaximumScore: 50},
		},
		AllowedScope: "repository",
		Template:     DefaultTemplate,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{
		"Primary refactoring guardrail — these rules take precedence", "do not game or merely redistribute Slopmark measurements",
		"numbered, part, or function shards", "cohesive, meaningfully named module",
		"do not apply a cosmetic quick fix; state the limitation explicitly in your final response",
		"Work only inside the workspace", "Slopmochi will measure", "Measurement context:",
		"Slopmark static code-quality measurements", "SCORE is Slopmark's weighted total",
		"This is a code refactor task using the 'slopmark' tool to monitor effectiveness",
		"Improve responsibilities and abstractions rather than moving complexity around.",
		"Required target checklist:", "do not stop after the first", "- one.go",
		"Measurements from the previous attempt", "{target_manifest}",
	} {
		if !strings.Contains(document.EffectiveBody(), wanted) {
			t.Fatalf("default template omitted %q: %q", wanted, document.EffectiveBody())
		}
	}
	if !strings.Contains(document.Objective, "{previous_attempt}") || !strings.Contains(document.Objective, "{target_manifest}") {
		t.Fatalf("runtime placeholders were not retained in the configured template: %q", document.Objective)
	}
	if !strings.HasPrefix(document.EffectiveBody(), GuardrailEnvelope) {
		t.Fatalf("trusted guardrail was not first: %q", document.EffectiveBody())
	}
	if document.Version != Version {
		t.Fatalf("compiled prompt version = %q, want %q", document.Version, Version)
	}
}

func TestCompilePrependsOnlyTheTrustedGuardrailWhenTemplateHasNoPlaceholders(t *testing.T) {
	t.Parallel()
	document, err := Compile(Input{
		Contract: fix.ScoringContract{
			Targets: []fix.TargetSnapshot{{Path: "one.go", Score: 90}, {Path: "two.go", Score: 80}, {Path: "three.go", Score: 70}},
			Goal:    fix.ScoringGoal{MaximumScore: 50},
		},
		AllowedScope: "targets-only", AllowedPaths: []fix.RepoPath{"one.go", "two.go", "three.go"},
		Template: "Improve the selected code.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := document.EffectiveBody(), GuardrailEnvelope+"\n\nImprove the selected code."; got != want {
		t.Fatalf("compiled prompt = %q, want exact configured template %q", got, want)
	}
}

func TestCompileRejectsMissingTargets(t *testing.T) {
	t.Parallel()
	if _, err := Compile(Input{}); err == nil {
		t.Fatal("missing targets were accepted")
	}
}

func TestCompileUsesCompactPromptForLargeTargetList(t *testing.T) {
	t.Parallel()
	targets := make([]fix.TargetSnapshot, 200)
	paths := make([]fix.RepoPath, len(targets))
	for index := range targets {
		path := fix.RepoPath(fmt.Sprintf("src/feature-%03d/a-deliberately-long-selected-filename-for-manifest.go", index))
		targets[index] = fix.TargetSnapshot{Path: path, Score: 90}
		paths[index] = path
	}
	if !RequiresTargetManifest(paths) {
		t.Fatal("large target list was not assigned a manifest")
	}
	document, err := Compile(Input{
		Contract:     fix.ScoringContract{Targets: targets, Goal: fix.ScoringGoal{MaximumScore: 50}},
		AllowedScope: "repository",
		Template:     "Improve targets from {targets}. Manifest: {target_manifest}. Count: {target_count}.\n{baseline_scores}",
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := document.EffectiveBody()
	if !strings.Contains(prompt, "{target_manifest}") || !strings.Contains(prompt, "Count: 200") {
		t.Fatalf("compact prompt omitted configurable manifest data: %q", prompt)
	}
	if strings.Contains(prompt, paths[100].String()) {
		t.Fatalf("large target list was expanded into the prompt: %q", prompt)
	}
	if len(prompt) > 2*1024 {
		t.Fatalf("compact prompt is unexpectedly large: %d bytes", len(prompt))
	}
}
