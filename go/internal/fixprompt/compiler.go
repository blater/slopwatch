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

const envelope = `You are operating inside a Slopwatch-owned candidate workspace.
Modify only the admitted scope. Do not create branches, commits, pushes, pull requests, waivers, suppressions, scoring configuration changes, or dead code intended to game metrics.
Preserve observable behavior and public APIs unless the objective explicitly requires a compatible change. Slopwatch independently verifies the result; your claim of success is not verification.`

type Input struct {
	Contract       fix.ScoringContract
	AllowedScope   string
	AllowedPaths   []fix.RepoPath
	ValidationPlan string
	Guidance       string
	DetachedBody   string
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
	objective := fmt.Sprintf(
		"Refactor the following target files so every target has SCORE <= %s: %s.\nAllowed change scope: %s.",
		number(input.Contract.Goal.MaximumScore), strings.Join(paths, ", "), input.AllowedScope,
	)
	if input.AllowedScope != "repository" {
		allowed := append([]fix.RepoPath(nil), input.AllowedPaths...)
		sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
		values := make([]string, len(allowed))
		for i, path := range allowed {
			values[i] = path.String()
		}
		objective += "\nExact admitted paths (targets plus planned supporting tests): " + strings.Join(values, ", ") + "."
	}
	if len(input.Contract.Goal.Focus) > 0 {
		focus := append([]fix.MetricGoal(nil), input.Contract.Goal.Focus...)
		sort.Slice(focus, func(i, j int) bool { return focus[i].Metric < focus[j].Metric })
		values := make([]string, len(focus))
		for index, goal := range focus {
			values[index] = fmt.Sprintf("%s <= %s", goal.Metric, number(goal.Maximum))
		}
		objective += "\nFocus requirements: " + strings.Join(values, ", ") + "."
	}
	if input.ValidationPlan != "" {
		objective += "\nRequired validation plan: " + input.ValidationPlan + "."
	}
	evidence := evidenceText(targets)
	return agent.InstructionDocument{
		Version: Version, Envelope: envelope, Objective: objective, Evidence: evidence,
		UserGuidance: strings.TrimSpace(input.Guidance), DetachedBody: strings.TrimSpace(input.DetachedBody),
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
