// Package fixprompt compiles structured remediation tasks into deterministic,
// versioned agent instructions. Runtime adapters transport the result without
// changing its policy.
package fixprompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

const Version = "slopwatch-fix/v1"

const DefaultTemplate = `Refactor {targets} until every file has a score of {target_score} or lower.
Focus on: {focus_metrics}.
You may edit: {allowed_paths}.
Current measurements:
{baseline_scores}
Keep observable behaviour and public APIs unchanged unless the task requires a compatible change.`

const envelope = `Work only inside the workspace and files named in this task.
Do not create branches, commits, pushes, pull requests, waivers, suppressions, scoring configuration changes, or dead code intended to game scores.
Slopwatch will measure the changed files and handle Git after you finish.`

type Input struct {
	Contract       fix.ScoringContract
	AllowedScope   string
	AllowedPaths   []fix.RepoPath
	ValidationPlan string
	BranchName     string
	Template       string
}

func Compile(input Input) (agent.InstructionDocument, error) {
	if len(input.Contract.Targets) == 0 {
		return agent.InstructionDocument{}, fmt.Errorf("compile fix prompt: at least one target is required")
	}
	if input.Contract.Goal.MaximumScore < 0 {
		return agent.InstructionDocument{}, fmt.Errorf("compile fix prompt: target score must be non-negative")
	}
	targets := append([]fix.TargetSnapshot(nil), input.Contract.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	paths := make([]string, len(targets))
	targetSet := make(map[fix.RepoPath]bool, len(targets))
	for index, target := range targets {
		paths[index] = target.Path.String()
		targetSet[target.Path] = true
	}
	switch input.AllowedScope {
	case "targets", "targets-only", "targets-and-tests":
		if len(input.AllowedPaths) == 0 {
			return agent.InstructionDocument{}, fmt.Errorf("compile fix prompt: exact admitted paths are required for scope %q", input.AllowedScope)
		}
		allowedSet := make(map[fix.RepoPath]bool, len(input.AllowedPaths))
		for _, path := range input.AllowedPaths {
			allowedSet[path] = true
		}
		for target := range targetSet {
			if !allowedSet[target] {
				return agent.InstructionDocument{}, fmt.Errorf("compile fix prompt: admitted paths omit target %q", target)
			}
		}
	case "repository":
	default:
		return agent.InstructionDocument{}, fmt.Errorf("compile fix prompt: unsupported change scope %q", input.AllowedScope)
	}
	allowedText := "the entire repository"
	if input.AllowedScope != "repository" {
		allowed := append([]fix.RepoPath(nil), input.AllowedPaths...)
		sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
		values := make([]string, len(allowed))
		for i, path := range allowed {
			values[i] = path.String()
		}
		allowedText = strings.Join(values, ", ")
	}
	focusText := "overall score"
	if len(input.Contract.Goal.Focus) > 0 {
		focus := append([]fix.MetricGoal(nil), input.Contract.Goal.Focus...)
		sort.Slice(focus, func(i, j int) bool { return focus[i].Metric < focus[j].Metric })
		values := make([]string, len(focus))
		for index, goal := range focus {
			values[index] = fmt.Sprintf("%s <= %s", goal.Metric, number(goal.Maximum))
		}
		focusText = strings.Join(values, ", ")
	}
	validationPlan := input.ValidationPlan
	if validationPlan == "" {
		validationPlan = "none"
	}
	evidence := evidenceText(targets)
	template := input.Template
	if strings.TrimSpace(template) == "" {
		template = DefaultTemplate
	}
	objective := strings.NewReplacer(
		"{targets}", strings.Join(paths, ", "),
		"{target_score}", number(input.Contract.Goal.MaximumScore),
		"{focus_metrics}", focusText,
		"{change_scope}", input.AllowedScope,
		"{allowed_paths}", allowedText,
		"{baseline_scores}", strings.TrimPrefix(evidence, "Baseline evidence:\n"),
		"{validation_plan}", validationPlan,
		"{branch}", input.BranchName,
	).Replace(template)
	return agent.InstructionDocument{
		Version: Version, Envelope: envelope, Objective: strings.TrimSpace(objective),
	}, nil
}

func evidenceText(targets []fix.TargetSnapshot) string {
	lines := []string{"Baseline evidence:"}
	for _, target := range targets {
		line := fmt.Sprintf("- %s: SCORE %s", target.Path, number(target.Score))
		metricIDs := make([]string, 0, len(target.Metrics))
		for id := range target.Metrics {
			metricIDs = append(metricIDs, string(id))
		}
		sort.Strings(metricIDs)
		for _, id := range metricIDs {
			metric := target.Metrics[fix.MetricID(id)]
			if metric.Complete {
				line += fmt.Sprintf(", %s %s", id, number(metric.Value))
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func number(value float64) string { return fmt.Sprintf("%g", value) }
