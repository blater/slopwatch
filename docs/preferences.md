# Preferences

The live dashboard stores user preferences in a versioned TOML file. TOML was
chosen because the hierarchy remains readable and editable without YAML's type
ambiguities or JSON's lack of comments.

Slopmochi keeps preferences and supporting persistent data under one root:

- Linux: `${XDG_CONFIG_HOME:-~/.config}/slopmochi/`
- Other platforms: `~/.slopmochi/`

The preferences file is `preferences.toml` in that directory. `fix-jobs.jsonl`
contains one structured record per fix, linking to its plain-text logfile in
`fix-jobs/`. The record contains the settings used and is updated with the
final status, touched files, and agent references. Each job logfile contains
the exact prompt, agent activity, and final outcome. Analysis data, worktrees,
and repository-specific settings are stored in subdirectories of the same
root. The preferences file is created with complete
defaults on the first dashboard launch. If it is deleted or unreadable,
Slopmochi replaces it with current defaults and continues.
Changes made through Settings, Columns, Weights, or Sort are written
immediately using an atomic file replacement. Hand edits are read on the next
launch. An explicitly supplied command-line option takes precedence for that
run; currently this applies to `--trend-window`.

Low-frequency agent implementation settings—including runtime, executable,
probe timing, cancellation grace, and provider resource
limits—are edited only in this file. The Agents popup is deliberately a
single-account provider chooser: it shows availability and the active provider,
then exposes only essential connection details and automatic readiness results.

The fix `prompt_template` is the complete agent prompt. Slopmochi substitutes
`{targets}`, `{target_score}`, `{focus_metrics}`, `{change_scope}`,
`{allowed_paths}`, `{baseline_scores}`, `{target_checklist}`, `{target_count}`,
`{target_manifest}`, `{target_manifest_count}`, `{previous_attempt}`, and
`{branch}`. No instruction text is added outside the template.

```toml
version = 1

[appearance]
theme = 'dark'

[table]
visible_columns = ['cog', 'npath', 'cyclo', 'deep', 'god', 'coupling']
sort_by = 'score'
sort_descending = true

[interaction]
trend_window = '10m0s'

[fix]
target_score = 100.0
focus = ['score']
change_scope = 'repository' # Any file in the project
profile = 'codex-default'
model = '' # selected adapter default
effort = 'high'
prompt_template = '''Work only inside the workspace. The named files are measurement targets, not a write allowlist.
You may change supporting project files when needed for a coherent refactor.
Do not create branches, commits, pushes, pull requests, waivers, suppressions, scoring configuration changes, or dead code intended to game scores.
Slopmochi will measure the changed files and handle Git after you finish.

Measurement context:
The values in this task are Slopmark static code-quality measurements reported by Slopmochi. Each baseline line belongs to the file named at the start of that line. Lower values are better.
SCORE is Slopmark's weighted total of the enabled measurements for that file; it is not the sum of the raw values shown.
COG means cognitive complexity; NPATH means possible execution paths; CYCLO means cyclomatic complexity; SHALLOW is the module-shallowness penalty; GOD is responsibility concentration; coupling counts referenced types; nesting means excessive control-flow nesting; type safety counts unsafe TypeScript findings.

This is a code refactor task using the 'slopmark' tool to monitor effectiveness. Refactor {targets} until every file has a score of {target_score} or lower.
Focus on: {focus_metrics}.
You may edit any project file needed for a coherent refactor, including creating,
moving, or splitting classes, functions, routines, and modules. However, we must refactor effectively, not move complexity around - do not increase the scores of other existing code by more than trivial amounts. Any new code must score low.
Current measurements:
{baseline_scores}
Keep observable behaviour and public APIs unchanged unless the task requires a compatible change.

Required target checklist:
Review and address every file below; do not stop after the first. Each file must meet the requested goal before you finish.
{target_checklist}

There are {target_count} selected target files. When a target manifest path is present below, read every newline-delimited filename from it before editing and report what was done for each target.
Target manifest: {target_manifest}

Measurements from the previous attempt, when present:
{previous_attempt}'''
branch_template = 'slopmochi/fix/{target-stem}-{job-short-id}'

[concurrency]
max_agents = 2
max_verifiers = 1
max_actors_per_job = 32
max_candidate_preview_bytes = 4194304
max_candidate_preview_lines = 5000

[[agents.profiles]]
id = 'codex-default'
label = 'Codex — managed sign-in (ChatGPT recommended)'
runtime = 'codex-cli'
executable = 'codex'
runtime_profile = 'slopmochi'
authentication_ref = 'provider-owned'

[agents.profiles.options]
probe_timeout = '15s'
termination_grace = '5s'

[[agents.profiles]]
id = 'gpt-default'
label = 'OpenAI Responses API — API key'
runtime = 'openai-responses'
authentication_ref = 'env:OPENAI_API_KEY'

[agents.profiles.options]
max_turns = '0'
max_tool_calls = '0'
max_output_tokens = '0'
max_context_tokens = '0'
# File, response, request, context, listing, probe and summary byte/entry
# budgets are configured here when non-default values are needed.

[delivery]
workspace = 'current-files' # current-files | worktree
git = 'uncommitted'         # uncommitted | current-branch | new-branch
publish = 'local'           # local | push | pull-request
remote = 'origin'
base_branch = 'main'
branch_template = 'slopmochi/fix/{target-stem}-{job-short-id}'
publisher = 'github-cli'
draft_pull_requests = true
command_output_bytes = 4194304
commit_title_template = 'Refactor {targets} with Slopmochi'
commit_body_template = 'Automated remediation for {goal}.'
pull_request_title_template = 'Refactor {targets} with Slopmochi'
pull_request_body_template = 'Automated remediation for {goal}.'

[scoring]
weight_step = 0.5
maximum_weight = 20.0

[scoring.components.ambiguous_boolean_expression]
enabled = false
weight = 4.0

[scoring.components.cognitive_complexity]
enabled = true
weight = 10.0

[scoring.components.coupling_between_objects]
enabled = true
weight = 10.0

[scoring.components.cyclomatic_class_complexity]
enabled = true
weight = 5.0

[scoring.components.cyclomatic_method_complexity]
enabled = true
weight = 5.0

[scoring.components.deeply_nested_if]
enabled = false
weight = 6.0

[scoring.components.explicit_any]
enabled = false
weight = 3.0

[scoring.components.god_class]
enabled = true
weight = 1.0

[scoring.components.module_shallowness]
enabled = true
weight = 5.0

[scoring.components.non_exhaustive_union]
enabled = false
weight = 8.0

[scoring.components.npath_complexity]
enabled = true
weight = 8.0

[scoring.components.unsafe_type_assertion]
enabled = false
weight = 5.0

[scoring.components.unsafe_type_boundary]
enabled = false
weight = 10.0

[scoring.components.unsafe_type_propagation]
enabled = false
weight = 4.0

[scoring.components.unsafe_type_use]
enabled = false
weight = 4.0
```

The three delivery defaults are independent. `uncommitted` always implies
`local`. Committing the current branch is available only when working in the
current files. A separate worktree can be left uncommitted or committed to a
new branch. Remote, base-branch and pull-request settings are used only when
the selected publish result needs them. `command_output_bytes` is an advanced
preferences-file setting and is deliberately absent from the UI.

`visible_columns` accepts `cog`, `npath`, `cyclo`, `deep`, `god`, `coupling`,
`nesting`, and `typesafety`. `sort_by` accepts those names plus `score` and
`filename`. Themes are `dark` and `light`; durations use Go duration syntax.

Agent-specific options are owned and validated by the selected runtime adapter
but are configured only in this preferences file. For the OpenAI Responses
adapter, `max_turns`, `max_tool_calls`, and token checks use `0` to mean no
Slopmochi-imposed budget (or provider default for output tokens). Byte and entry
budgets must be positive. Slopmochi does not impose an attempt count or job
wall-clock timeout; iterations are automatic and per-job Cancel remains
available. The Codex
App Server adapter's readiness timeout and post-cancellation termination grace
are also preferences-file-only. Probe timeout governs readiness checks only;
termination grace starts only after the user or owning context cancels work.
Neither disconnects a live fix attempt.

Transcripts are process-local display data and are not copied into persisted
job state. Candidate source monitoring uses the pinned preview byte and line settings;
larger files are explicitly labelled truncated instead of being rejected by a
compiled 4 MiB/5,000-line ceiling.

Settings › Git & pull requests controls whether new PRs are drafts or
ready for review, the user-selected
base, the organisation's branch template, and the shared candidate/delivery/
publisher command-output budget. These commands have no Slopmochi wall-clock
timeout and remain cancelable.

Small supervisor escalation waits after cancellation are fixed safety
invariants, not work budgets: once a user has cancelled an operation, they
bound cleanup before a wedged child/supervisor is killed. They cannot activate
while an operation is live and are labelled as such in the implementation.

Preference precedence is resolved internally without adding source or origin
hints to Settings rows. Repository preferences may narrow concurrency,
retention and delivery policy,
but cannot broaden authority or replace user-owned agents, credentials,
executables, remotes, prompts or publishers.

The file exposes presentation preferences and the dashboard's score mixture.
Analyzer thresholds, formulas, and caps remain versioned in
`component-catalog.json`: changing them alters the meaning of analyzer output
and therefore requires cache-key and evidence-contract handling rather than a
presentation preference.
