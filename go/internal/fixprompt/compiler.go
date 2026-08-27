// Package fixprompt compiles structured remediation tasks into deterministic,
// versioned agent instructions. Runtime adapters transport the result without
// changing its policy.
package fixprompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/fix"
)

const Version = "slopmochi-fix/v2"

const inlineTargetBytes = 8 * 1024

// GuardrailEnvelope is trusted Slopmochi policy, not part of the editable
// master template. It is always the first text sent to the agent so saved or
// repository-provided templates cannot accidentally remove the primary
// protection against metric-gaming refactors.
const GuardrailEnvelope = `Primary refactoring guardrail — these rules take precedence over any later instruction to reach a measurement target:
Improve the underlying design; never treat moving code into more files as a refactor by itself, and do not game or merely redistribute Slopmark measurements.
Do not lower a target's score by moving unchanged or lightly edited code into numbered, part, or function shards, making one file per declaration, or leaving the target empty or as a package/import shell.
Create or split source files only when each resulting file is a cohesive, meaningfully named module with a distinct responsibility and the change reduces underlying complexity instead of relocating it. Keep the number of new files to the minimum justified by the design.
Treat every changed or newly created source file as part of the refactor's quality outcome. If the requested score cannot be reached without violating these rules, do not apply a cosmetic quick fix; state the limitation explicitly in your final response.`

const DefaultTemplate = `Work only inside the workspace. The named files are measurement targets, not a write allowlist.
You may change supporting project files when needed for a coherent refactor.
Do not create branches, commits, pushes, pull requests, waivers, suppressions, scoring configuration changes, or dead code intended to game scores.
Slopmochi will measure the changed files and handle Git after you finish.

Measurement context:
The values in this task are Slopmark static code-quality measurements reported by Slopmochi. Each baseline line belongs to the file named at the start of that line. Lower values are better.
SCORE is Slopmark's weighted total of the enabled measurements for that file; it is not the sum of the raw values shown.
COG means cognitive complexity; NPATH means possible execution paths; CYCLO means cyclomatic complexity; SHALLOW is the module-shallowness penalty; GOD is responsibility concentration; coupling counts referenced types; nesting means excessive control-flow nesting; type safety counts unsafe TypeScript findings.

This is a code refactor task using the 'slopmark' tool to monitor effectiveness. Refactor {targets} until every file has a score of {target_score} or lower.
Focus on: {focus_metrics}.
You may edit any project file needed for a coherent refactor. Improve responsibilities and abstractions rather than moving complexity around. Do not increase the scores of other existing code by more than trivial amounts. Any new code must score low.
Current measurements:
{baseline_scores}
Keep observable behaviour and public APIs unchanged unless the task requires a compatible change.

Required target checklist:
Review and address every file below; do not stop after the first. Each file must meet the requested goal before you finish.
{target_checklist}

There are {target_count} selected target files. When a target manifest path is present below, read every newline-delimited filename from it before editing and report what was done for each target.
Target manifest: {target_manifest}

Measurements from the previous attempt, when present:
{previous_attempt}`

type Input struct {
	Contract     fix.ScoringContract
	AllowedScope string
	AllowedPaths []fix.RepoPath
	BranchName   string
	Template     string
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
	targetPaths := make([]fix.RepoPath, len(targets))
	targetSet := make(map[fix.RepoPath]bool, len(targets))
	for index, target := range targets {
		paths[index] = target.Path.String()
		targetPaths[index] = target.Path
		targetSet[target.Path] = true
	}
	largeTargetList := RequiresTargetManifest(targetPaths)
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
	allowedText := ""
	if input.AllowedScope != "repository" {
		allowed := append([]fix.RepoPath(nil), input.AllowedPaths...)
		sort.Slice(allowed, func(i, j int) bool { return allowed[i] < allowed[j] })
		values := make([]string, len(allowed))
		for i, path := range allowed {
			values[i] = path.String()
		}
		allowedText = strings.Join(values, ", ")
	}
	focusText := ""
	if len(input.Contract.Goal.Focus) > 0 {
		focus := append([]fix.MetricGoal(nil), input.Contract.Goal.Focus...)
		sort.Slice(focus, func(i, j int) bool { return focus[i].Metric < focus[j].Metric })
		values := make([]string, len(focus))
		for index, goal := range focus {
			values[index] = fmt.Sprintf("%s <= %s", goal.Metric, number(goal.Maximum))
		}
		focusText = strings.Join(values, ", ")
	}
	targetText := strings.Join(paths, ", ")
	evidence := evidenceText(targets)
	checklist := targetChecklist(targets)
	if largeTargetList {
		targetText = "{target_manifest}"
		evidence = ""
		checklist = ""
	}
	template := input.Template
	if strings.TrimSpace(template) == "" {
		template = DefaultTemplate
	}
	objective := strings.NewReplacer(
		"{targets}", targetText,
		"{target_score}", number(input.Contract.Goal.MaximumScore),
		"{focus_metrics}", focusText,
		"{change_scope}", input.AllowedScope,
		"{allowed_paths}", allowedText,
		"{baseline_scores}", evidence,
		"{target_checklist}", checklist,
		"{target_count}", fmt.Sprintf("%d", len(targets)),
		"{branch}", input.BranchName,
	).Replace(template)
	return agent.InstructionDocument{
		Version: Version, Envelope: GuardrailEnvelope, Objective: strings.TrimSpace(objective),
	}, nil
}

// RequiresTargetManifest chooses the transport format only. It never limits,
// truncates, or splits the selected target set.
func RequiresTargetManifest(paths []fix.RepoPath) bool {
	bytes := 0
	for _, path := range paths {
		bytes += len(path.String()) + 1
	}
	return bytes > inlineTargetBytes
}

func targetChecklist(targets []fix.TargetSnapshot) string {
	lines := make([]string, 0, len(targets))
	for _, target := range targets {
		lines = append(lines, "- "+target.Path.String())
	}
	return strings.Join(lines, "\n")
}

func evidenceText(targets []fix.TargetSnapshot) string {
	lines := make([]string, 0, len(targets))
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
