# Preferences

The live dashboard stores user preferences in a versioned TOML file. TOML was
chosen because the hierarchy remains readable and editable without YAML's type
ambiguities or JSON's lack of comments.

The platform-default location is:

- macOS: `~/Library/Application Support/slopwatch/preferences.toml`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/slopwatch/preferences.toml`
- Windows: `%AppData%\slopwatch\preferences.toml`

The file is created with complete defaults on the first dashboard launch.
Changes made through Settings, Columns, Weights, or Sort are written
immediately using an atomic file replacement. Hand edits are read on the next
launch. An explicitly supplied command-line option takes precedence for that
run; currently this applies to `--trend-window`.

Low-frequency agent implementation settings—including runtime, executable,
probe timing, cancellation grace, confinement roots, and provider resource
limits—are edited only in this file. The Agents popup is deliberately a
single-account provider chooser: it shows availability and the active provider,
then exposes only essential connection details and automatic readiness results.

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
focus = []
change_scope = 'targets-and-tests'
profile = 'codex-default'
model = '' # selected adapter default
effort = 'high'
delegation = 'single'
prompt_template = 'default'
branch_template = 'slopwatch/fix/{target-stem}-{job-short-id}'
validation_plan = ''

[concurrency]
max_agents = 2
max_verifiers = 1
max_retained_jobs = 100
max_transcript_bytes = 1048576
max_actors_per_job = 32
max_candidate_preview_bytes = 4194304
max_candidate_preview_lines = 5000

[validation_workspace]
max_files = 100000
max_directories = 20000
max_path_bytes = 16777216
max_file_bytes = 67108864
max_total_bytes = 536870912
container_pids = 256
container_memory_bytes = 4294967296
container_cpu_millis = 2000
container_temporary_bytes = 1073741824
container_workspace_bytes = 1073741824
container_nofile_limit = 1024
container_generated_file_bytes = 67108864
container_stop_timeout = '3s'
container_control_timeout = '30s'
container_sentinel_timeout = '10s'
container_crash_probe_timeout = '15s'

[[agents.profiles]]
id = 'codex-default'
label = 'Codex — managed sign-in (ChatGPT recommended)'
runtime = 'codex-cli'
executable = 'codex'
runtime_profile = 'slopwatch'
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
default_mode = 'candidate'
remote = 'origin'
base_branch = 'main'
branch_template = 'slopwatch/fix/{target-stem}-{job-short-id}'
publisher = 'github-cli'
draft_pull_requests = true
require_validation = false
command_output_bytes = 4194304
commit_policy = 'on-publish'
commit_title_template = 'Refactor {targets} with Slopwatch'
commit_body_template = 'Automated remediation for {goal}.'
pull_request_title_template = 'Refactor {targets} with Slopwatch'
pull_request_body_template = 'Automated remediation for {goal}.'
cleanup_policy = 'retain'

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

`visible_columns` accepts `cog`, `npath`, `cyclo`, `deep`, `god`, `coupling`,
`nesting`, and `typesafety`. `sort_by` accepts those names plus `score` and
`filename`. Themes are `dark` and `light`; durations use Go duration syntax.

Agent-specific options are owned and validated by the selected runtime adapter
but are configured only in this preferences file. For the OpenAI Responses
adapter, `max_turns`, `max_tool_calls`, and token checks use `0` to mean no
Slopwatch-imposed budget (or provider default for output tokens). Byte and entry
budgets must be positive. Slopwatch does not impose an attempt count or job
wall-clock timeout; Retry and per-job Cancel remain explicit actions. The Codex
App Server adapter's readiness timeout and post-cancellation termination grace
are also preferences-file-only. Probe timeout governs readiness checks only;
termination grace starts only after the user or owning context cancels work.
Neither disconnects a live fix attempt.

`concurrency.max_transcript_bytes` is enforced exactly per job as the sum of
the JSON-encoded transcript entries retained for display and recovery. Saving a
smaller value trims the oldest entries immediately; an entry larger than the
configured budget is not retained. There is no hidden minimum or maximum.
Candidate source monitoring uses the pinned preview byte and line settings;
larger files are explicitly labelled truncated instead of being rejected by a
compiled 4 MiB/5,000-line ceiling.

Validation plans are trusted user/installation definitions. Settings ›
Validation can select a plan and edit each check's required status, timeout and
output budget. The same panel exposes the file, directory, path-byte,
per-file-byte and total-byte ceilings used identically for the confined
candidate copy and the before/after fingerprint; exceeding one fails validation
with the named effective value. Repository preferences may select trusted plan
IDs but cannot author executable commands or broaden these user-owned workspace
limits. The same panel exposes finite validation-container process, memory, CPU,
tmpfs, open-file, generated-file, cleanup and readiness-probe policy. Timeout
labels distinguish active validation checks from Docker lifecycle/readiness
operations; none is an undisclosed fix-job timeout. Settings › Git & pull
requests controls whether new PRs are drafts or
ready for review, whether passing validation is mandatory, the user-selected
base, the organisation's branch template, and the shared candidate/delivery/
publisher command-output budget. These commands have no Slopwatch wall-clock
timeout and remain cancelable.

Small supervisor escalation waits after cancellation are fixed safety
invariants, not work budgets: once a user has cancelled an operation, they
bound cleanup before a wedged child/supervisor is killed. They cannot activate
while an operation is live and are labelled as such in the implementation.

Every constraint shown in Settings includes its effective origin. Repository
preferences may narrow concurrency, retention, validation and delivery policy,
but cannot broaden authority or replace user-owned agents, credentials,
executables, remotes, prompts or publishers.

The file exposes presentation preferences and the dashboard's score mixture.
Analyzer thresholds, formulas, and caps remain versioned in
`component-catalog.json`: changing them alters the meaning of analyzer output
and therefore requires cache-key and evidence-contract handling rather than a
presentation preference.
