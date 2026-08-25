# Agent-assisted fix plan

Status: implemented integration candidate; final senior review and empirical
runtime release gates are recorded below. This plan refines the initial
`x`-to-fix feature and records the architectural constraints agreed during
adversarial review.

## Implementation and release-gate record

The initial implementation includes the provider-neutral runtime registry,
Codex CLI strategy, isolated candidate worktrees, fresh scoring verification,
durable concurrent job controller, retained candidate review, journaled Git
publication saga, GitHub CLI draft-PR publisher, Fix form, Agents dashboard,
per-job actions, and the associated settings surfaces. The analysis cache and
preferences document are supplied through typed application ports; runtime
adapters do not access either directly.

The Codex CLI `0.149.1` protocol probes on the current macOS development host
confirmed non-interactive authentication and model discovery, but the exact
tool sandbox did not deny every outside-workspace write and Git metadata read.
The process-group supervisor also cannot prove containment of a descendant
that escapes into a new session. The adapter therefore reports `degraded` and
cannot mutate on this host. This is the intended fail-closed result of the
release gate, not a user-overridable warning. The adapter and its fake-runtime
contract tests remain available for a platform confinement implementation
that can prove all gates.

Configured validation commands are likewise fail-closed in production until
a platform executor proves candidate-only writes, denied Git/sensitive reads,
default-deny tool networking, bounded output/time, and crash containment.
Candidate-only review remains usable wherever an agent runtime passes its
gate; pull-request publication additionally requires a configured validation
plan to pass.

## Senior review record

The plan was reviewed independently from three veto perspectives:

- senior Go/Bubble Tea architecture and repository integration;
- concurrent orchestration, process/Git security and crash recovery;
- terminal interaction design, accessibility and responsive layout.

The first expanded draft was not approved. The reviewers identified missing
contracts around analysis, application commands, workspace identity, runtime
confinement, lifecycle/retry, crash recovery, publication idempotency and TUI
presentation. Two adversarial revision passes resolved every veto, and all
three reviewers approved the resulting plan. The normative resolutions are
incorporated below. Phase 2 may not begin until the Phase 0/1 contract tests
and platform/runtime feasibility gates prove them.

The assembled implementation received a second review at PR time. The terminal
UX slice was approved, and the application/provider architecture was approved,
including frozen path scope, metric completeness, typed delivery, provider-owned
settings schemas and repairable configuration. Release as a runnable Fix feature
was vetoed: the current production composition truthfully declares Codex crash
containment unavailable and uses a deny-all validation executor. Consequently,
this branch is a disabled-foundation integration candidate until a platform
executor proves both confinement contracts. A draft PR must preserve that
distinction and must not describe the end-to-end mutation path as complete.

## Outcome

From the follow dashboard, `x` opens a fix dialog for the highlighted file.
The user can set an overall target score, choose focus metrics, select a
configured coding-agent profile, model, effort and delegation mode, choose a
Git delivery workflow, and optionally edit the generated instructions.

Running the fix creates a bounded remediation job. Slopwatch supplies the
agent runtime with a structured task, observes progress, independently
reanalyzes the result, and presents the verified score and diff for review.
An agent exiting successfully or claiming that the target was reached is not
proof of compliance.

The domain uses `[]Target` from the first version even though the initial UI
supplies one highlighted file. This avoids redesigning the service when
multi-selection is added.

## Product decisions

The recommended initial decisions are:

- `x` fixes the highlighted file; Space is reserved for future multi-select.
- Multiple fix jobs may be submitted, run, monitored and canceled
  independently. Active jobs continue while the user navigates the dashboard,
  configures another fix, or inspects another job.
- Concurrent jobs use separate isolated Git worktrees. Direct concurrent
  mutation of the dashboard's live workspace is not supported.
- Agent execution and verification use separate bounded resource pools. A
  saturated pool queues a job; it never prevents the user from submitting it.
- Success requires a fresh Slopwatch analysis against the scoring contract
  captured when the job starts.
- Cached reports and evidence may be used to construct the baseline task, but
  the agent layer never reads the analysis cache.
- The initial runtime integrations are non-interactive coding-agent CLIs. A
  raw model API is not an agent runtime and is not registered directly.
- The agent runtime never creates branches, commits, pushes, or pull requests.
- Git publication is a separate workflow performed after verified changes are
  reviewable.
- Provider-specific capability differences are retained rather than flattened
  into a misleading lowest-common-denominator configuration.
- No shell command template is used as the common provider abstraction.
- Files and Agents are peer dashboard views. The Agents view is the global job
  control surface and replaces a separate job-center overlay.
- V1 requires a completely clean Git repository, rejects follow-symlink fix
  targets and unsupported submodule/filter layouts, and creates detached
  candidates at a pinned commit.
- V1 defaults are target score `100`, `targets-and-tests` change scope, two
  concurrent agents, one verifier, cancel-all-and-quit, and no automatic
  candidate/worktree deletion.
- The first real runtime is Codex CLI only if an implementation spike proves
  the required non-interactive protocol, write confinement and process-tree
  cancellation. No adapter is grandfathered around those requirements.
- The initial publisher is GitHub CLI and creates draft PRs. Publication is a
  later phase and is never performed by the agent runtime.

## Dependency direction

```text
analysis/cache
    |  immutable domain snapshot
    v
fix coordinator -----> scoring verifier
    |
    | typed AgentRequest
    v
agent Runtime interface
    |
    +----> Codex CLI strategy
    +----> Claude CLI strategy
    +----> future tool-loop strategy ----> raw model API clients

fix coordinator -----> workspace/Git workflow -----> PR publisher

follow TUI -----> fix application service
```

`internal/agent` and its provider adapters must not import `analysiscache`,
`native`, `report`, or `follow`. The fix coordinator translates analysis data
into a provider-neutral task before calling the runtime.

The follow TUI must not construct provider commands, parse provider events,
handle credentials, manage Git, or determine whether a score passed.

## Deep agent-runtime interface

The agent interaction slice is a strategy registry behind a small, cohesive
interface. It is a runtime protocol, not merely a shared Go method signature.
Exact names may change during implementation, but the boundary must retain
these semantics:

```go
type Strategy interface {
    Probe(context.Context, ResolvedProfile) ProbeResult
    Execute(
        context.Context,
        ResolvedProfile,
        AgentRequest,
        EventSink,
    ) AgentResult
}

type AgentRequest struct {
    JobID       JobID
    AttemptID   AttemptID
    Workspace   Workspace
    Task        RemediationTask
    Model       ModelID
    Effort      EffortID
    Delegation  DelegationMode
    WritePolicy WritePolicy
    Limits      Limits
    Resume      ResumeToken
}

type RemediationTask struct {
    Targets      []TargetSnapshot
    Goal         ScoringGoal
    Evidence     []MetricEvidence
    Instructions InstructionDocument
    Validation   ValidationContract
}
```

`Probe` is read-only. It performs availability, version,
authentication-readiness and capability discovery. `Execute` is blocking and
represents exactly one mutating attempt. It owns provider-specific invocation,
stdin or protocol encoding, event parsing, model and effort mapping,
cancellation, resume support, and result normalization. Cancellation is driven
by the context and must terminate the child process group before `Execute`
returns.

The registry is keyed by stable runtime kinds such as `codex-cli` and
`claude-cli`. Each strategy advertises typed options:

- supported and default models;
- supported effort levels;
- delegation modes;
- structured progress and resume support;
- sandbox and path-restriction guarantees;
- network requirements;
- authentication readiness.

The dialog shows or disables controls from these capabilities. A delegation
switch is not shown for a runtime that cannot genuinely provide delegation.
A prose request to "use a team" is not treated as equivalent to a runtime
capability.

If a trusted custom-command runtime is added later, it is a clearly labelled
strategy using an executable and argument array. It does not use a shell or
become the abstraction implemented by built-in adapters.

### Profile and registry contract

`ResolvedProfile` is produced by the preferences boundary. It is not the
properties document and contains no unresolved inheritance or precedence. It
contains a stable profile ID, runtime kind, executable location, typed or
namespaced adapter options, an authentication reference, and permission
defaults.

The registry maps a runtime kind to one strategy. Registration is explicit at
composition time; repository preferences cannot register implementations.
Duplicate kinds are startup errors. Unknown kinds are configuration errors,
not a reason to fall back to another provider.

An adapter validates its own namespaced profile options. The generic framework
validates stable IDs, paths, trust scope and secret-reference shape. A profile
is fingerprinted so probe results can be cached and invalidated when its
configuration changes.

The strategy receives secret resolution and process-launch facilities through
constructor dependencies. It does not read preferences, environment files,
keychains or provider auth files through generic filesystem access. A CLI
adapter may deliberately delegate authentication to the provider CLI without
reading its credential material.

### Probe and capabilities contract

`ProbeResult` is data, not an error-only readiness check. It contains:

- runtime kind, detected version and compatibility status;
- ready, unavailable, unauthenticated or degraded state;
- actionable, redacted diagnostics;
- the dynamically available `Capabilities` for this resolved profile.

`Capabilities` contains option sets, not assumed universal enums:

```go
type Capabilities struct {
    Models        []Option[ModelID]
    Efforts       []Option[EffortID]
    Delegation    []Option[DelegationMode]
    WritePolicies []WritePolicy
    Resume        bool
    Progress      ProgressCapability
    Network       NetworkCapability
    PathControl   PathControlCapability
    Isolation     RuntimeIsolation
}
```

Model and effort IDs are opaque stable values advertised by that strategy.
The TUI does not maintain a cross-provider list or guess aliases. Defaults are
identified in the option set. Before every execution the coordinator checks
the request against the latest compatible probe result, and the adapter checks
again before launching.

Probe may execute only documented read-only version/auth/model-discovery
operations. It must not modify the workspace, trigger an agent turn, start an
interactive login, or cause a billable inference request merely to render the
dialog.

### Request contract

`AgentRequest` is immutable for an attempt. Its workspace is an absolute,
canonical directory prepared by the workspace strategy. Target paths are
normalized, workspace-relative paths that have already been checked against
traversal and symlink escape.

The request contains a structured `InstructionDocument`, not a cache handle
and not an adapter-authored remediation policy. The fix coordinator owns the
meaning and versioning of safety, objective, evidence and user-guidance
sections. Each adapter owns only transport encoding: for example, joining the
sections for a CLI prompt or mapping them to supported message roles.

The adapter must not:

- query Slopwatch analysis or preference storage;
- reinterpret the target score or decide that compliance was achieved;
- widen paths, permissions, network access or delegation beyond the request;
- perform Git branch, commit, push or PR operations;
- silently substitute a model, effort or delegation mode;
- add provider-specific prompt instructions that weaken the supplied policy.

If the adapter cannot represent a requested control faithfully, it returns an
unsupported-capability result before launching anything.

### Event contract

`EventSink` accepts normalized events only. Provider-native output is retained
in a separate bounded, redacted diagnostic transcript and is never exposed as
the application's state model.

Each event contains job ID, attempt ID, monotonically increasing sequence,
timestamp, kind, short summary and optional typed details. Initial event kinds
are:

- `started`;
- `activity`;
- `command_started` and `command_finished`;
- `file_changed`;
- `usage`;
- `warning`;
- `runtime_message`.

Command events carry a stable command ID so start and finish are correlated
without parsing text. Usage events state whether each field is a cumulative
attempt total or a delta; the normalized v1 projection stores cumulative
totals to prevent double counting. Actor-capable events carry stable actor and
optional parent-actor IDs. Provider identifiers and summaries are length
bounded before entering the service.

Events are informational and cannot declare score compliance. There is exactly
one returned terminal `AgentResult`; no separate terminal event is required.
Events emitted after the result are contract violations and are ignored by the
coordinator.

The sink is synchronous and must return quickly. The coordinator owns the
bounded queue between runtime and TUI. When presentation cannot keep up,
coalescible activity events may be dropped or combined, but warnings,
file-change notifications and the terminal result remain durable. Provider
output is stripped of control sequences before it reaches the UI.

### Result and error contract

`AgentResult` always identifies the job and attempt and has one terminal
status:

- `completed`: the runtime completed normally; this does not mean the scoring
  goal passed;
- `failed`: launch, provider, protocol or non-zero process failure;
- `canceled`: context cancellation was observed and the process tree stopped;
- `timed_out`: the attempt limit expired and the process tree stopped.

It may contain a provider session reference, normalized usage, an exit code,
redacted diagnostics and a short agent summary. Provider session references
are opaque, scoped to runtime kind, profile fingerprint and workspace, and are
never placed in prompts.

Failure diagnostics use typed classes: unavailable runtime, incompatible
version, unauthenticated, invalid profile, unsupported capability, launch
failure, protocol failure, provider failure, cancellation and timeout. The TUI
maps these classes to actions without parsing strings.

An adapter never automatically retries a mutating execution. A transport
failure can occur after files were changed, so blind retry is not idempotent.
The coordinator inspects the workspace and decides whether a new attempt or a
resume is safe. Read-only probes may use bounded retries.

### Cancellation and shutdown contract

Context cancellation begins adapter shutdown. The adapter stops accepting new
provider work, requests graceful termination when supported, waits for a
bounded grace period, terminates the whole child process group, drains bounded
output, and only then returns `canceled` or `timed_out`.

Closing the TUI cancels every owned execution and waits for runtime shutdown.
It must not leave a child process editing the retained worktree. Late events
are rejected by job and attempt ID. Platform-specific process-group behavior
is isolated behind the injected process launcher and covered by OS-specific
tests.

### Resume and delegation contract

Resume is optional. A resume token can be used only when the current strategy
advertises resume support and its runtime kind, profile fingerprint and
workspace match. Otherwise the coordinator starts a new attempt with explicit
verification feedback. Resume never bypasses the attempt, time or permission
limits.

Delegation is also capability-driven. `single` is the portable baseline.
Provider-native team modes appear as advertised options with clear labels and
limits. The coordinator does not emulate a team by launching several runtimes
concurrently in the first version.

### Runtime framework acceptance tests

Every built-in strategy must pass the same black-box contract suite using a
fake executable or transport:

- probe is read-only and reports readiness/capabilities deterministically;
- invalid profiles and unsupported options fail before process launch;
- request paths cannot escape the prepared workspace;
- invocation uses executable plus argv and stdin, never a shell;
- events are normalized, ordered and associated with the correct attempt;
- unknown non-critical provider events do not crash the adapter;
- malformed required protocol data produces a typed protocol failure;
- stdout/stderr limits, control-sequence removal and secret redaction hold;
- cancellation and timeout stop the process tree before return;
- crash simulation proves supervisor/parent-death cleanup and lease recovery;
- tool subprocesses cannot write outside the candidate, read denied
  secret/state roots, inherit transport authentication or exceed network
  policy;
- ambiguous failures are not retried automatically;
- returned session references cannot be reused across a different profile or
  workspace;
- the strategy never invokes Git publication or reads Slopwatch caches and
  properties.

Registered strategies are safe for concurrent calls and hold no mutable
invocation state. Provider state belongs to the invocation object created by
`Execute`; alternatively the registry stores a factory that creates one
strategy instance per attempt. The contract suite executes multiple attempts
concurrently to detect cross-job state, output and cancellation leaks.

## Concurrent fix-job manager

Agent runtimes execute attempts; they do not own job scheduling or lifecycle.
A concurrency-safe fix application service is the only boundary used by the
follow TUI:

```go
type Service interface {
    Prepare(context.Context, PrepareRequest) (FixDraft, error)
    Submit(context.Context, SubmitRequest) (JobID, error)

    Jobs(JobFilter) JobListSnapshot
    Job(JobID) (JobSnapshot, bool)
    Subscribe() Subscription

    Execute(context.Context, JobCommand) (CommandReceipt, error)
    CandidateFile(context.Context, JobID, RepoPath) (CandidateFile, error)
    Diff(context.Context, JobID, DiffRequest) (DiffPage, error)
    Transcript(context.Context, JobID, LogCursor, int) (LogPage, error)

    Shutdown(context.Context) error
}

type Subscription interface {
    Wait(context.Context, GlobalRevision) (GlobalRevision, error)
    Close() error
}

type JobCommand struct {
    RequestID        CommandID
    JobID            JobID
    ExpectedRevision uint64
    Action           JobAction
    Parameters       ActionParameters
}
```

`Prepare` resolves baseline analysis, preferences and runtime capabilities for
an editable draft. `Submit` revalidates the finalized draft and durably admits
the job. The context passed to `Submit` covers validation and admission only;
the service creates and owns the execution context afterward, so the job does
not end when the Bubble Tea command returns.

`Execute` is the single typed command port for cancel, retry, resume, publish,
acknowledge-conflict, keep, archive and discard. Snapshots enumerate their
currently allowed actions. Commands include a client-generated idempotency ID
and expected job revision; stale or duplicate commands return typed receipts
and never act on whichever row currently occupies an index.

`Subscription.Wait` is level-triggered by a monotonic global revision. It
returns immediately when the service revision is newer than the caller's
revision; otherwise it blocks until a change or context cancellation. The TUI
creates one long-lived subscription, handles a wake by fetching authoritative
snapshots, and then waits with the last observed revision. A change between
snapshot retrieval and the next wait therefore causes an immediate wake rather
than being lost. Closing returns a stable `subscription closed` result and the
TUI does not reissue a wait, preventing command spin.

Candidate files, diffs and transcripts are paged/read separately and never
embedded wholesale in `JobSnapshot`. This bounds model copying and lets source
views address an explicit candidate workspace without importing Git or runtime
details into `follow`.

The service owns:

- controller-owned job records keyed by immutable job ID;
- per-job contexts, attempt IDs, workspaces, runtime instances and verifiers;
- separate bounded schedulers for agent execution and verification;
- normalized event ingestion and per-job snapshot projection;
- job journals, diagnostic transcript locations and retained candidates;
- individual cancellation and coordinated application shutdown;
- collision detection across active target and changed-file sets.

There is no global `activeJob`, `agentRunning` or single current-agent state.
The TUI may hold a currently viewed job ID, but that selection has no effect on
the execution of other jobs.

For v1, one controller goroutine is the sole writer of lifecycle, scheduling,
reservation and snapshot state. Runtime, verification, candidate and
publication workers send typed results tagged with job and attempt IDs. Public
snapshots clone maps and slices before publication. Bubble Tea model state is
mutated only inside `Update`; service callbacks never mutate the TUI model.

### Scheduling and resource isolation

The scheduler has independent resource limits:

- maximum simultaneously running agent attempts;
- maximum simultaneous fresh verifications;
- optional per-profile or per-runtime limits for provider quotas;
- a retained-job limit that prevents an unbounded in-memory history.

The recommended initial defaults are two concurrent agent attempts and one
concurrent verification. They are preferences, not constants in the TUI.
Reaching a limit places an admitted job in a visible queue. Agent slots are
released when `Execute` has stopped/drained the runtime process tree; a job
waiting for a verifier does not continue consuming an agent slot.

One primary runtime attempt consumes one scheduler agent slot. Provider-native
delegated actors remain part of that attempt rather than becoming hidden
Slopwatch jobs; `Limits.MaxActors` bounds them and the runtime must enforce or
reject the request. The UI shows their telemetry when available. A future
weighted scheduler may account for provider-native teams, but v1 does not
pretend it can independently pause or schedule those actors.

Scheduling is stable FIFO within each explicit priority class. User commands
and shutdown/cancel transitions are serviced promptly, but repeated urgent
traffic cannot starve the oldest runnable background job. Admission checks and
target reservations are one controller transaction: no two concurrent submit
calls can both observe a path as free. A queued job pins its base revision,
target hashes, resolved preferences and scoring contract at admission. Runtime
readiness is probed again immediately before launch so a long queue cannot use
stale authentication or capabilities.

Capacity is reserved before dispatch and released exactly once. An agent slot
is released only after the full child process tree has stopped and bounded
stdout/stderr have been drained; a merely exited parent is insufficient. A
verifier slot follows the same rule for analyzer workers. The scheduler emits
queue position and limiting-resource reason in the snapshot without promising
an ETA.

Every concurrent mutating job has its own detached worktree, candidate
identity, runtime process tree, output limits and verifier. Main-dashboard
analysis, job verification and provider output cannot share mutable buffers or
cancellation contexts.

### Per-job lifecycle

Each job independently follows:

```text
admitted -> queued -> preparing -> running
         -> waiting_verifier -> verifying -> awaiting_review
         -> publishing -> reconciling -> completed

active phase -> canceling -> awaiting_action
active phase failure -> awaiting_action
awaiting_action -> queued                 retry or resume
awaiting_review -> publishing             publish
awaiting_review -> completed              keep local candidate
quiescent phase -> archived               hide, retain owned artifacts
quiescent phase -> discarded              explicitly delete owned artifacts
```

An admitted job is immutable identity plus an append-only series of attempts,
candidate states and publication attempts. Each attempt has its own ID and
terminal runtime result. `awaiting_action` is a retry-capable quiescent phase,
not a terminal failure. Its typed issue may be canceled, runtime failure,
verification failure, noncompliance, scope violation, conflict or ambiguous
external state.

Job phase is not overloaded with independent outcomes. Every snapshot carries:

- compliance: unknown, compliant or noncompliant;
- validation: not configured, not run, passed or failed;
- scope: unknown, clean, violated or conflicted;
- delivery: none, local candidate, committed, pushed, PR created or ambiguous;
- retention: none, retained, archived or discarded;
- attention: none, information, action required or blocking.

`awaiting_review` means a candidate is stable and inspectable, not necessarily
publishable. It may be noncompliant or have failed/unconfigured validation.
`AllowedActions` expresses whether policy permits retry, keep, publish,
archive or discard. Publication initially requires complete compliant scoring,
clean scope, acknowledged cross-job warnings and the configured validation
policy.

Candidate-only delivery reaches `completed` only when the user chooses Keep
candidate. Archive hides a job without deleting its owned branch/worktree;
Discard is the only UI action that requests deletion. Terminal operational
phases are `completed`, `archived` and `discarded`; retry creates a new attempt
only from `awaiting_action` or `awaiting_review`.

State-transition invariants are:

- the single service controller is the sole transition writer for every job;
  workers return tagged immutable results and never own or mutate job state;
- revisions increase on every observable snapshot change;
- terminal jobs never return to an active state;
- late attempt events are rejected by job and attempt ID;
- `awaiting_review` requires a retained inspectable candidate;
- `completed` means the selected delivery workflow finished, not merely that
  an agent process exited;
- score compliance is recorded separately from validation and publication.

### Individual cancellation

Cancellation never targets a runtime globally or affects another job. It is an
idempotent `JobCommand`. It linearizes against the current cancelable
operation's durable terminal decision: the attempt result while running, the
queue-dispatch decision while queued, workspace result while preparing,
verifier-dispatch decision while waiting, verification result while verifying,
and the current publication-step result while publishing/reconciling. A
terminal result from an earlier phase does not prevent cancellation of the
current one. Quiescent phases are cancel-ineligible and omit Cancel from
`AllowedActions`.

When cancel wins that phase race, the controller journals `cancel_requested`
before signaling workers. The command receipt means the request was admitted,
not that shutdown has finished; the UI watches the job until it leaves
`canceling`.

Phase behavior is:

- queued cancellation is journaled before its queue position/capacity
  reservation is released, then the job enters retryable `awaiting_action`
  with canceled issue; its target reservation remains until completed
  delivery, Archive or Discard;
- workspace preparation stops and unused resources are cleaned up;
- a running agent enters `canceling` until its process tree has stopped;
- verification cancellation stops only that verifier and retains the
  candidate worktree and diff for inspection;
- publication cancellation stops future steps, then reconciles whether a
  commit, push or PR already occurred before reporting external state.

If runtime completion races with an already accepted cancel request, the job
becomes canceled-with-retained-candidate in `awaiting_action`; it does not
silently proceed to verification or compliance. The command caller's context
bounds command admission only and never reverses an accepted cancellation.

Partial changes are retained by default after cancellation. The user may
inspect, retry/resume, archive or explicitly discard them. Cancellation may
proceed while new jobs are submitted and other jobs continue running.

### Target and diff collision policy

The initial policy rejects a new job whose declared target set overlaps a
reserved target set. A reservation begins with durable admission and remains
while a candidate or retryable job is retained, including awaiting review,
failed and canceled attempts. It is released only after completed delivery,
Archive, or Discard. Starting a deliberate competing fix is a future explicit
action rather than an accidental second press of `x`.

The collision key is canonical repository identity plus normalized,
repository-relative logical path. Normalization follows the repository
filesystem's case behavior and retains rename aliases, so a case-only rename
or renamed target cannot evade a reservation.

Non-overlapping targets may run concurrently. Their allowed supporting-file
scopes can still produce intersecting diffs. The manager continually
reconciles each worktree's actual diff and flags cross-job intersections as a
conflict risk. This is advisory: absence of a shared path does not prove
semantic independence. The candidates remain independently inspectable, but
publishing either requires an acknowledgement journaled against the exact pair
of candidate diff hashes. Any diff change invalidates that acknowledgement.

### Manager contract tests

- Jobs continue after their setup or monitor view closes.
- A new job can be submitted while others run, verify, cancel or await review.
- Saturated resource pools queue rather than reject valid jobs.
- Agent and verifier limits are enforced independently without slot leaks.
- Stable FIFO ordering does not starve an old job under repeated submissions
  and control commands; reservations are atomic under concurrent admission.
- Canceling one queued, running, verifying or publishing job leaves every
  other job unchanged.
- Completion/cancel races linearize to the first durable decision and release
  each scheduler/resource reservation exactly once.
- A canceled queued job continues blocking overlapping admission until
  completed delivery, Archive or Discard releases its target reservation.
- A runtime cannot emit state into another job or an obsolete attempt.
- Reopening a monitor reconstructs the complete view from `JobSnapshot`.
- A level-triggered subscription cannot lose a change between snapshot fetch
  and the next wait, and closing it cannot produce a command spin.
- Duplicate/stale commands are idempotent and never affect a replacement row.
- Overlapping declared targets are rejected deterministically.
- Intersecting collateral diffs produce durable conflict warnings.
- Snapshots are deep immutable copies; paged logs/diffs remain bounded.
- Shutdown cancels and joins every owned process; no agent is orphaned.
- Concurrent journal and snapshot updates remain race-free under `go test
  -race`.

## Analysis and scoring boundary

Fix orchestration depends on an application-facing analysis port, not on the
dashboard analyzer, report model or on-disk cache:

```go
type AnalysisService interface {
    PrepareBaseline(context.Context, BaselineRequest) (BaselineSnapshot, error)
    Verify(context.Context, VerificationRequest) (VerificationResult, error)
}

type ValidationService interface {
    Validate(
        context.Context,
        CandidateIdentity,
        ValidationPlan,
    ) (ValidationResult, error)
}
```

`PrepareBaseline` resolves the analysis root and returns a complete immutable
snapshot for the pinned target content. It may use the analysis subsystem's
cache internally. It rejects rather than silently filling gaps when the cache
is stale, a target is missing, the analyzer catalog changed, or evidence
needed by a selected metric is incomplete; it may synchronously rerun analysis
to obtain a complete baseline. `Verify` always creates an analyzer rooted at
the candidate analysis root and performs a fresh analysis. A persistent shared
cache is disabled for final verification; an implementation may use a job-local
cache only when its keys include the frozen analyzer/scoring identity and
candidate content fingerprint.

`AnalysisService.Verify` performs analysis and scoring only. `ValidationService`
owns configured tests/build checks; `fixapp` sequences them and records scoring
and validation as separate outcomes. `ValidationPlan` is a frozen list of
trusted check IDs, executable-plus-argv vectors, validated candidate-relative
working directories and per-check limits—never shell text. Built-ins and
user-owned preferences may define checks. Repository preferences may select a
pre-registered check by stable ID but cannot define an executable, arguments,
environment, network access or broader permissions.

The adapter between existing analysis code and this port is the only fix
package permitted to import `native`, `report` or analysis-cache types. Shared
metric catalog, weighting and goal-evaluation logic currently embedded in
dashboard formatting moves into a UI-independent `internal/scoring`
package. Both the dashboard projection and verification consume that package,
so displayed and enforced definitions cannot drift. `fixapp`, `agent` and the
TUI receive immutable fix-domain DTOs and never analysis implementation types.

Workspace identity is explicit because Slopwatch may be launched below a Git
root and a candidate worktree may have a different analysis subdirectory:

```go
type WorkspaceIdentity struct {
    Repository       RepositoryID
    RepositoryRoot   string // canonical original checkout root
    AnalysisRoot     string // canonical root represented by dashboard paths
    BaseCommit       ObjectID
}

type CandidateIdentity struct {
    Repository       RepositoryID
    RepositoryRoot   string // canonical detached worktree root
    AnalysisRoot     string // corresponding root for candidate analysis
}
```

All target and diff paths use a validated `RepoPath`, relative to the Git root.
Analysis paths are derived by the analysis boundary; no component guesses by
joining a provider path to the process working directory.

## Scoring contract

The coordinator freezes an immutable scoring contract at durable admission:

- selected paths and content hashes;
- baseline Git revision and workspace identity;
- cached baseline scores, component values and relevant evidence;
- analysis profile/catalog identity;
- enabled components and active weights;
- aggregate score constraint;
- selected focus metrics and their exact verifier expressions;
- completeness requirement;
- permitted regressions, if any, in metrics outside the focus set.

The disk analysis cache remains owned by the analysis layer. That layer
supplies the coordinator with a typed snapshot. Adapters receive only the
resulting `AgentRequest`; they receive no cache path, cache key requiring
resolution, or cache API.

Current dashboard columns do not all have identical aggregation semantics.
For example, a displayed metric may be a maximum, sum or grouped set of
components while SCORE uses weighted contributions. Each selectable focus
metric therefore requires one documented verifier expression. "Optimize COG"
must not be left as an untestable prompt phrase.

Verification succeeds only when all of the following are true:

- every declared target has a complete result and `SCORE <= target`;
- every selected focus-metric predicate passes its documented expression;
- no non-focus metric exceeds its permitted regression tolerance;
- every changed path is classified and the change-scope policy passes;
- the configured validation policy passes, when publication requires it;
- the target and candidate content fingerprints remain unchanged from the
  start through completion of analysis and validation.

Deleted or renamed declared targets fail v1 verification. A supporting-file
change is included in scope and regression evaluation even when it has no
per-file score. If the filesystem fingerprint changes during verification,
the result is discarded and the job returns to waiting for a stable verifier;
bounded repeated mutation becomes a typed failure. Cache entries, provider
claims and intermediate watcher results are never accepted as final proof.

## Prompt contract

Instructions are versioned and assembled from structured data in three
layers:

1. A Slopwatch-owned, non-editable execution and safety envelope.
2. A generated objective and evidence section derived from the scoring
   contract.
3. User-editable additional guidance.

Advanced editing applies to the user-editable guidance or generated task body;
it cannot remove the external safety and verification envelope. Once a user
detaches the body from form generation, the UI shows that state and offers
"Reset from controls".

The task includes the target paths, baseline evidence, exact acceptance
conditions, allowed modification scope, behavioral/API preservation policy,
validation expectations, and iteration limits. It also instructs the runtime
not to change scoring configuration, add waivers or suppressions, game metrics
with dead code, or perform Git publication.

Branch names, executable paths and credentials are orchestration data and are
not included in the prompt. Prompts are passed through stdin or a private
temporary file rather than as a command-line argument.

## Fix setup UX

The fix setup dialog is transient; admitted jobs are background tasks and do
not depend on the dialog remaining open. Other job updates continue to be
processed while this or any other overlay is visible.

The form is a full-screen surface when width is below 60 columns or height is
below 16 rows; otherwise it is a
centered overlay with an internally scrolling body and fixed title/actions. It
uses these stable sections:

1. **Goal**: immutable targets/baseline, target SCORE and multi-select focus
   metrics limited to measurements available for those targets.
2. **Execution**: agent profile, capability-derived model, effort, delegation,
   attempt/time limits and resolved confinement/network summary.
3. **Changes**: allowed `targets-and-tests` scope and validation policy.
4. **Delivery**: candidate/local-branch/PR mode and an editable proposed branch
   name when relevant. Its default is rendered from the configured convention;
   validation is asynchronous and repeated at admission/publication.
5. **Instructions**: custom guidance, generated-body preview and advanced
   editing state.
6. **Preflight**: baseline freshness, profile readiness, runtime compatibility,
   workspace safety, collision and policy diagnostics.
7. A final focusable **Run fix** action, enabled only when all required
   asynchronous and synchronous checks pass.

Up/down moves fields, left/right changes single choices, Space toggles a
multi-choice, Enter edits/activates, `P` previews the effective instruction
document and Esc returns without admission. Footer hints always match the
focused control. Invalid controls retain focus and show a nearby actionable
message; validation is not conveyed by color alone.

Profile probing and baseline preparation are asynchronous and cancellable.
Dependent controls show `Checking…` and are disabled while their matching
draft revision is unresolved. Results carry a draft/probe ID so changing the
profile cannot apply a late result from the old choice. Opening Settings ->
Agents from the form preserves the draft and returns to the same field after
save/cancel; the new preference revision triggers a new probe.

Advanced has two intentional levels. Custom guidance is freely editable. The
generated task body is read-only in Preview unless the user chooses **Detach
and edit**, confirms that form controls will no longer regenerate it, and then
sees a persistent `DETACHED` label. **Reset from controls** is confirmed when
it would lose edits. The Slopwatch safety/verification envelope is always
locked. Multiline editing uses a full-screen editor; Ctrl-S applies, while Esc
with dirty text offers Save, Discard or Continue editing.

The target paths are snapshotted when the dialog opens. A watcher refresh or
rerank must not silently change them. Submission either returns an admitted
job ID or leaves the populated draft open with actionable validation errors.
Once admitted, the dialog closes or transitions to that job's monitor; the
user may immediately return to Files and submit another job.

The current set of modal booleans is replaced by `MainView` plus a typed
overlay stack before fix UI is added. The top overlay alone handles key input;
all non-key job, probe, resize and analysis messages continue through normal
`Update` processing. Each overlay records its caller so Settings returns to an
intact fix draft, Logs returns to its monitor, and candidate source/diff
returns to the exact Agents selection. All model mutation remains inside
Bubble Tea `Update`; service goroutines only complete commands with typed data.

## UI observability model

Observability has four layers:

1. An aggregate top-bar status visible in either main view.
2. An Agents table showing all current fix jobs.
3. A detailed monitor for one selected job.
4. A bounded diagnostic log for forensic provider output.

The interface distinguishes runtime activity from Slopwatch-owned state.
Runtime messages such as "done" or a successful process exit never render as
score compliance. The job moves through a visible verification phase, and
only the verifier can produce `COMPLIANT` or `NOT COMPLIANT`.

No private model reasoning or hidden chain of thought is displayed. The UI may
show user-directed agent messages, observable tool activity, commands, file
changes, delegation topology, usage and outcomes.

The application service supplies a TUI-facing presentation projection so
`follow` does not duplicate lifecycle policy:

```go
type JobPresentation struct {
    ID              JobID
    Revision        uint64
    Phase           PresentationPhase
    Attention       Attention
    ProfileLabel    string
    ModelLabel      string
    EffortLabel     string
    Goal            string
    Targets         []TargetPresentation
    CurrentAction   string
    ActorCount      int
    WarningCount    int
    ConflictCount   int
    Timing          TimingPresentation
    AllowedActions  []JobAction
}
```

The canonical user-visible phase vocabulary is `QUEUED`, `PREFLIGHT`,
`PREPARING`, `RUNNING`, `WAITING`, `VERIFYING`, `REVIEW`, `PUBLISHING`,
`CANCELING`, `RECONCILING`, `FAILED`, `CONFLICT`, `CANCELED` and `DONE`.
Glyphs and color may supplement but never replace these words in an available
detail or help surface. `AllowedActions`, evaluated against the snapshot
revision, is the only source for action enablement.

### Files and Agents as peer views

The main dashboard has two peer views. Main chrome remains exactly two lines:
the existing top status line, then a line combining the active tab with the
table header. The viewport retains the existing `height-3` calculation (two
chrome lines plus footer), avoiding an extra permanent status row. Top-line
priority is workspace/analyzer error, then fix action-required status, fix
aggregate, scan/cache progress, and ordinary repository/workspace status.

The peer views are:

```text
[FILES]   AGENTS
```

`Tab` toggles between them and `A` jumps directly to Agents. Each view retains
its own cursor, stable selected row ID, vertical offset, horizontal offset,
sort, filter and expansion state. Returning to Files restores the user's prior
file selection even when analysis or job updates reordered either table.

The Agents view replaces a separate global job-center overlay. Its top-level
row represents one fix job and primary runtime session, not an individual
provider-native subagent. This is stable for providers that do not expose
their internal actors. When actor telemetry exists, a compact team count is
shown and the detailed monitor exposes the actor tree.

The aggregate top bar is compact and textual as well as colored:

```text
-=[slopwatch]=-  FIX 5 · 2 RUNNING · 1 VERIFYING · 2 REVIEW
```

Counts with zero values are omitted. Action-required states such as failed,
conflicted or review-ready remain visible until acknowledged or archived.
Activity is never represented by color alone.

### Agents table

The wide-terminal layout is:

```text
-=[slopwatch]=-  FIX 5 · 2 RUNNING · 1 VERIFYING · 2 REVIEW
 FILES [AGENTS]  STATE       AGENT          GOAL          TARGETS ACTIVITY TIME
▾  RUNNING     Codex / high      SCORE ≤100 · COG,CPL 2 files Running tests  2:14
      T  internal/service.go       SCORE 142→118◇  COG 18→11  CPL 9→8
      T  internal/parser.go        SCORE 126→91✓   COG 14→8   CPL 7→5
      S  internal/service_test.go  supporting file · modified

▸  VERIFYING   Claude / medium   SCORE ≤80 · NPATH    1 file  Analyzing       4:03
▸  REVIEW      Codex / high      SCORE ≤100 · SHALLOW 3 files Ready           8:12
▸  QUEUED      Claude / low      SCORE ≤120 · COG     1 file Agent slot 3/4  0:31
```

The disclosure glyph is always present and communicates expansion only.
Selection continues to use the standard selected-row background; no text
cursor indicates selection. Multiple job rows may be expanded simultaneously.
Expansion is keyed by job ID and survives sorting and live updates.

Default sort order prioritizes attention:

1. failed, conflicted or otherwise awaiting user action;
2. review ready;
3. verifying or waiting for verification;
4. running;
5. queued or preparing;
6. completed and archived.

Users may sort by state, agent profile, goal, target, time or latest activity.
Find searches job ID, runtime/profile label, target paths, goal text, branch
name and activity summary. Completed jobs are retained for the session and by
the journal until archived; an `active/all` filter controls their visibility.

The job summary row contains:

- textual phase, with a fixed-width state glyph only when space permits;
- agent profile, model or effort only while space permits;
- compact goal (`SCORE ≤ N` plus focus metric names);
- declared target count and first path where space permits;
- normalized current activity;
- elapsed time for active jobs or time since completion for terminal jobs;
- team actor count, warnings or conflict badges when applicable.

Responsive behavior is specified by terminal size rather than left to ad-hoc
column clipping:

- at 96 columns and above, render the full table;
- at 60–95 columns, omit time, detailed activity, target preview, model and
  effort as needed, in that order;
- at 36–59 columns, render each logical job/file row as a two-line block;
- below 36 columns or below 6 rows, show a fit-to-terminal resize screen; only
  resize-safe global navigation and the compact confirmed quit flow remain.

Forms and monitors become full-screen below 60 columns or 16 rows. A selected
logical row may occupy multiple visual lines; selection background covers all
of them, paging uses line spans, and sorting/updates keep the selected stable
ID at its prior top-relative visual line when it still fits; otherwise they
scroll by the minimum required to make the full logical row visible. Expanded file rows retain
SCORE and focus metrics and use horizontal scrolling. Disclosure/status glyphs
have terminal-width tests, with text always available.

### Expanded job files

Expanded rows show the authoritative union of declared targets and the actual
worktree diff. Files are classified:

- `T`: declared target;
- `S`: allowed supporting or collateral file changed by the agent;
- `!`: changed file outside the permitted scope;
- `?`: provisionally reported change not yet reconciled with the Git diff.

Provider `file_changed` events provide responsive provisional updates. The
coordinator periodically and at every phase boundary reconciles them against
the worktree diff. Provider events alone never remove a changed file from the
snapshot or establish scope compliance.

Expanded file rows are selectable. Their contextual actions are:

- `v`: view the candidate version from that job's worktree;
- `d`: view the file diff;
- `i`: view baseline and verified metric evidence;
- `Enter`: open the job monitor focused on that file.

Candidate source and diff views receive an explicit job workspace from the fix
service. They do not concatenate a provider-supplied path onto the dashboard
workspace.

Compact metrics show SCORE, the job's focus metrics, and any non-focus metric
that regressed beyond allowed tolerance. They never show a score calculated
from an arbitrary half-written state:

```text
SCORE 142 → …       modified, not verified
SCORE 142 → 118◇    latest independent checkpoint
SCORE 142 → 91✓     final verified result
```

Checkpoints exist only when the coordinator deliberately runs verification
between attempts. The glyph is supplemented by accessible text in detail and
help views. Supporting files without applicable measurements show `-`, not
zero. A file deleted or renamed by the candidate is labeled explicitly rather
than disappearing from the expanded list.

### Main-view commands

In Files:

- `x` on a file with no job reservation opens a new Fix dialog;
- `x` on a file whose target is reserved opens the highest-attention reserving
  job, including Review, failed or canceled `awaiting_action` states;
- Space remains reserved for future multi-select.

Files adds a fixed-width `FIX` column whose textual codes summarize the most
relevant retained job for that target: `QUE`, `PFL`, `PRE`, `RUN`, `WTG`,
`VER`, `REV`, `PUB`, `CNL`, `REC`, `ERR`, `CNF`, `CAN`, `DON` or `ARC`.
Blank means no retained job. It does not overload the score marker or rely on
a spinner. If several historical jobs exist, a reservation holder wins, then
the highest-attention/latest job; the Info surface lists the rest.

The initial collision policy prevents accidental duplicate reservation-holding
target jobs.
A future explicit competing-fix action may relax this without changing `x`.

The complete initial key map is:

| Context | Keys |
| --- | --- |
| Global | `Tab` switch Files/Agents, `A` Agents, `s` Settings, `h` Help, `q` Quit |
| Files | `x` Fix/open active job, `v` Source, `i` Info, `o` Sort, `r` Rescan |
| Agents job | `Enter` expand, `i` Monitor, `d` Diff, `l` Logs, `C` Cancel |
| Agents file | `Enter` monitor focused on file, `v` candidate source, `d` file diff, `i` metrics |
| Monitor | `[`/`]` previous/next visible job, `Enter` Actions, `d` Diff, `l` Logs, `C` Cancel |
| Lists/readers | `j`/`k` or arrows, `g`/`G`, Page Up/Down; left/right scroll horizontally |

Agents uses `a` to toggle Active/All. Retry, Resume, Publish, Keep, Archive,
Discard, acknowledge-conflict and cleanup are listed in an Actions sheet built
from `AllowedActions`, rather than consuming permanent single-letter bindings.
`Enter` opens that sheet from the monitor. When no actions are allowed it opens
a non-selectable explanation of the current phase and offers only Back.
Destructive or externally visible actions require confirmation. The
confirmation captures job ID, expected revision and relevant diff hash, then
rechecks eligibility before execution; it never targets the current row by
index. Inside a non-editing overlay, `q` behaves as Esc/back rather than
quitting. A focused text input consumes all printable runes, including `q`.

Find temporarily expands jobs containing matching file rows and restores the
user's prior expansion state when cleared. If the selected child is visible
while its parent summary has scrolled away, a sticky parent breadcrumb shows
job phase/profile/goal. Empty states explain how to start a fix, how to toggle
All, or why no find results match.

Footer hints are contextual, put `Tab` first on main views, and follow existing
display standards. Sorting and live updates preserve selection by ID and its
visual line where possible.

### Per-job monitor

The detailed monitor is a dismissible overlay. `Esc` backgrounds it while the
job continues; selecting the job in Agents reopens it from the authoritative
snapshot. The monitor never owns the execution lifetime.

```text
┌─ FIX JOB · RUNNING ─────────────────────────────────────────────┐
│ internal/service.go                                  02:14      │
│ SCORE 142.0 → ≤100.0        Focus: COG, CPL      Attempt 1/3   │
│                                                                │
│ ✓ Preflight  ✓ Workspace  ● Agent  ○ Verify  ○ Review          │
│                                                                │
│ CURRENT                                                        │
│ Running: go test ./internal/service/...              00:18     │
│                                                                │
│ ACTIVITY                                                       │
│ 14:31  Read 7 related files                                    │
│ 14:32  Edited internal/service.go                   +24 −31     │
│ 14:33  Edited internal/service_test.go               +9  −2     │
│ 14:34  Running go test ./internal/service/...                  │
│                                                                │
│ Files 2   Commands 4   Elapsed 02:14   Usage 31k tokens        │
│                                                                │
│ Esc back  Enter actions  l logs  d diff  C cancel              │
└────────────────────────────────────────────────────────────────┘
```

The monitor uses a phase stepper rather than a fabricated percentage or ETA.
It keeps baseline, target, focus metrics and attempt count visible. Current
activity is a normalized action, not raw terminal output. Start and completion
events update one action row; repetitive reads or searches are coalesced.
Warnings, scope violations and failures are pinned instead of scrolling away.
Authoritative Slopwatch phase, compliance, validation, scope and allowed
actions always appear above provider activity; runtime chatter can never push
the decision state off-screen.

Usage and monetary cost are shown only when the runtime reports them reliably.
The UI does not estimate cost from incomplete or provider-incomparable data.
The monitor can cycle to the previous or next visible job without returning to
Agents while preserving each job's selected activity and scroll position.

When provider-native actor telemetry exists, the monitor adds a compact tree:

```text
AGENTS
● primary       Editing service.go
├─ ✓ reviewer   Review complete
└─ ● tests      Running targeted tests
```

Normalized events carry an actor ID and optional parent actor ID. Providers
without this telemetry expose one `primary` actor. The UI never infers an actor
tree from prose.

### Activity projection and update rate

The fix service converts normalized runtime events, verifier transitions and
publication transitions into `JobSnapshot`. The TUI does not parse provider
messages to infer state. It renders snapshot revisions and may retain only
ephemeral selection/scroll state locally.

Coalescible activity causes at most one UI wake per 100 milliseconds. User
input, phase transitions, warnings, cancellation and terminal results wake
immediately. Activity events may be summarized under pressure, but state transitions,
warnings, changed-file reconciliation, cancellation and terminal results are
durable. Reopening a view always catches up from the latest snapshot rather
than relying on every visual update having rendered.

### Logs, failures and cancellation

`l` opens a separate scrollable diagnostic view. It contains bounded,
sanitized and redacted runtime output. It is diagnostic evidence, not the
state source. The header states whether output was truncated and gives the
private transcript location when retained.

A failed monitor preserves the typed reason, last meaningful activity,
candidate files and diff, worktree/branch location, verification state and
safe next actions. Retry or resume is offered only after the coordinator
inspects the retained workspace.

Cancellation visibly enters `CANCELING`; it does not display `CANCELED` until
the relevant process tree or verifier has stopped. Canceling one job never
changes another job's view or scheduler position. During publication, an
interrupted job displays `RECONCILING PUBLICATION` until remote state is known.

Quitting with active jobs opens a summary confirmation offering return to the
dashboard or cancel-all-and-quit. Detached background agents are not supported
in the first version. Once cancel-all-and-quit starts, the UI shows joined
progress and remains responsive; a second Ctrl-C does not bypass process-tree
shutdown and journal flushing. If the bounded shutdown deadline expires, it
reports the specific processes/artifacts needing attention rather than exiting
as though cancellation succeeded.

On the resize screen, `q` remains available. With no active jobs it quits; with
active jobs and at least 24 columns by 2 rows it shows this two-line
confirmation (with the actual count):

```text
3 jobs active
q cancel+quit · Esc stay
```

Confirmation starts the same joined shutdown flow and never exposes other
destructive actions. Below 24 columns or 2 rows it displays resize-only
guidance and ignores destructive confirmation.

### UI observability acceptance tests

- Files and Agents retain independent selection, scrolling, sorting and
  filtering through repeated switches and live reorderings.
- A user can submit a new fix while other jobs run, verify or cancel.
- Multiple jobs expand simultaneously and retain expansion by job ID.
- Job and file selection remain stable by ID, never by stale row index.
- Runtime completion displays verification in progress, never compliance.
- Only independently verified checkpoints and final values appear as scores.
- Provisional file events reconcile to the actual diff and cannot hide a
  changed or out-of-scope file.
- Cancel targets exactly the selected job and remains visibly pending until
  shutdown completes.
- Narrow layouts preserve state, agent and goal without overlapping footer
  hints or mismeasuring disclosure/status glyphs.
- Reopening a monitor reconstructs current content from its snapshot after
  skipped/coalesced UI events.
- Provider output cannot inject terminal control sequences, escape the popup,
  forge status text or expose a configured secret.
- Every color-coded state also has a textual or symbolic distinction.
- Rendering and interaction tests cover at least `24x8`, `36x8`, `60x16`,
  `80x24` and `120x30`, including job/probe updates arriving while each dialog,
  editor, diff, log and confirmation overlay is active.

## Workspace and Git workflow

Runtime execution, candidate-workspace management, delivery and PR publishing
are separate cohesive components. The runtime cannot call the delivery or
publisher interfaces and receives no publication credentials.

Every admitted mutating job receives a private detached Git worktree created
at its pinned base commit in Slopwatch-owned private application storage. The
dashboard checkout is never the agent workspace. This is required even when
only one job is running, and permits multiple agents and multiple prospective
PRs to remain in flight concurrently without sharing working files. The job
owns the worktree until explicit cleanup; PR creation does not delete it.
Branches do not exist during remediation. An eligible branch is created from
the exact verified candidate commit only when publication begins.

The Git service resolves identity with Git itself: canonical result of
`git rev-parse --show-toplevel`, canonical Git common directory,
`HEAD^{commit}`, and filesystem identity where available. It supports
Slopwatch being launched from an analysis directory below the repository root.
It does not reuse the dashboard's display-oriented repository identity.

Before durable admission, v1 requires a completely clean index and worktree,
including no untracked files, no in-progress merge/rebase/cherry-pick and no
dirty submodule state. The job pins the repository identity, base commit,
target blob IDs/content hashes, analysis/scoring identity and resolved
preferences. Those invariants are rechecked before worktree creation and
runtime launch. V1 rejects target paths that traverse or resolve through a
symlink, and repositories whose relevant paths use submodules, Git LFS or
content filters until their semantics are deliberately supported.

A repository-scoped inter-process mutation lease is keyed by the canonical
Git common directory. One Slopwatch process holds it while it owns mutable fix
candidates or performs publication; another process may inspect but cannot
start fix mutations and receives an actionable explanation. Within the owning
process, Git administrative operations such as worktree add/remove, ref
creation, commit and push are serialized per repository. Agent execution in
already-created worktrees remains concurrent.

"Fix selected file" means the file is the primary target, not necessarily the
only file that may change. The default `targets-and-tests` scope includes the
declared targets and test files classified in their analysis units. All
changes are shown. A v1 change outside the admitted scope blocks publication;
the user can inspect it and retry with a deliberately changed specification,
but cannot approve a scope escape after the fact.

The candidate-workspace component owns:

- detached worktree creation at the pinned commit;
- canonical path validation and candidate identity;
- diff inventory and scope-policy enforcement;
- proof that an artifact was created and remains owned by the job;
- retained-candidate discovery and safe cleanup.

Cleanup first reconciles `git worktree list --porcelain` and the journal. It
removes only a worktree whose canonical path, repository common directory and
job ownership marker all agree. It never uses force to remove an unknown,
dirty or mismatched worktree. Archive hides the job but retains artifacts;
Discard is the explicit destructive action. After a PR is merged or closed, a
future reconciled cleanup action may offer removal but never performs it
silently.

The delivery workflow owns:

- base revision and remote resolution;
- branch-name validation and collision handling;
- commit creation and signing/hook policy;
- exact-ref push and publication reconciliation.

The PR publisher owns provider-specific draft-PR creation and lookup. The
initial implementation uses GitHub CLI. Branch names are validated with Git;
an existing local or remote branch is never reused, reset or overwritten.
Agent-created hooks and repository hooks are not run, and non-interactive v1
does not require commit signing. If policy requires hooks or signing, publish
is unavailable with a clear explanation rather than prompting mid-job.

The built-in branch convention is
`slopwatch/fix/{target-stem}-{job-short-id}`. Template inputs are a fixed typed
set (`target-stem`, `job-short-id`, `date` and selected metric names), are
sanitized before rendering, and cannot execute functions or shell expansion.
The proposed value is editable in the setup form and pinned at admission, but
because the branch is created only after review its local and remote
availability is checked again at publication. A late collision returns to
Review with **Change branch name**; Slopwatch never silently chooses a different
published ref.

Publication is an idempotent journaled saga:

1. journal publication intent and the verified diff/content fingerprints;
2. create a commit and journal its exact object ID;
3. atomically create the absent local ref at that object ID and journal it;
4. atomically create the absent remote ref at that object ID, verify the remote
   value, and journal the ref;
5. create a draft PR and journal its provider repository and PR identity;
6. reconcile remote state before retrying any step whose response was lost.

Local ref creation uses compare-and-swap against the zero object ID. Remote ref
creation uses a provider create-ref API that fails when the ref
exists, or Git's compare-and-swap/force-with-lease form constrained specifically
to “ref must be absent”; it is never a general force push. A pre-existing or
racing ref becomes action-required/ambiguous and is not updated. The workflow
then reads the remote ref back and requires it to equal the journaled commit.

Retries search by the journaled commit/ref/PR identity and cannot create a
second PR for the same publication attempt. Cancellation stops future steps
but does not pretend an acknowledged remote action was undone. Slopwatch never
auto-deletes a remote branch or PR. Several published jobs may remain open at
once. Their local worktrees stay independent; overlapping retained or open-PR
diffs remain visible as conflict warnings, and acknowledgement is bound to the
exact base, head and diff hashes.

No workflow force-pushes, auto-merges, or discards a candidate without an
explicit eligible user action. Branch fields are conditional on workflow
mode and are previewed before admission.

## Preferences/properties integration

A separate PR will provide the preferences/properties file. This feature
depends on its API, not its file format. The TUI, fix coordinator and agent
adapters do not read or write that file directly.

The integration boundary provides a resolved, typed snapshot and scoped
save operations. A fix job pins the resolved snapshot used at start, so later
preference edits cannot change a running job.

The fix feature consumes a narrow adapter owned at composition time, even if
the incoming PR exposes a concrete document type:

```go
type PreferencesResolver interface {
    Resolve(context.Context, WorkspaceIdentity) (ResolvedPreferences, error)
}

type PreferencesStore interface {
    Save(
        context.Context,
        PreferenceScope,
        PreferencePatch,
        ExpectedPreferenceRevision,
    ) (SavedPreferences, error)
}
```

`ResolvedPreferences` is deeply immutable from the consumer's perspective and
includes value origins, revision, schema version and resolved agent profiles.
The settings application layer alone uses `PreferencesStore`; running jobs and
runtime adapters see only their pinned typed projection. Optimistic revision
checking prevents one settings dialog from overwriting a newer save.

### Priority definitions

- **Must**: required for a safe first integrated version; no compromise.
- **Should**: expected for v1, but may follow the first end-to-end vertical
  slice when omission is visible and recoverable.
- **Could**: useful follow-up with no architectural dependency.
- **Won't yet**: deliberately outside the first version.

### Must

- A typed API that hides file location, syntax and I/O from consumers.
- A schema version and explicit rejection of unsupported future versions.
- Validation before values become a resolved configuration.
- Deterministic precedence between built-ins, user preferences, repository
  preferences, CLI overrides and session edits.
- Origin metadata for every resolved value so Phase 1 settings can display
  inheritance and implement Reset to inherited correctly.
- An immutable resolved snapshot suitable for pinning to a job journal.
- Stable IDs for agent profiles and runtime kinds.
- Namespaced adapter-specific properties validated by the selected adapter.
- User-owned configuration for executable locations, authentication method,
  secret references and permission policy.
- Repository configuration cannot define executable paths, literal commands,
  credential sources or broader permissions.
- Literal API keys, access tokens and provider authentication files are never
  stored in properties. Only provider-owned auth, keychain identifiers or
  environment-variable references are allowed.
- Invalid or unavailable profiles produce actionable diagnostics and cannot
  launch a job.
- Sensitive values are redacted from errors, UI state and logs.
- A test/in-memory implementation so fix-domain and TUI tests do not depend on
  the physical properties file.

### Should

- Atomic replacement when saving preferences. This is not required to prove
  the initial read-only/in-memory vertical slice, but becomes a **Must before
  the settings UI is considered stable**, because interruption during direct
  overwrite can truncate the user's entire configuration. The incoming
  preferences implementation already uses private temporary-file replacement,
  so this is expected to be satisfied by the dependency rather than added to
  the fix feature's critical path.
- Restrictive file permissions where supported, even though secrets
  themselves are prohibited.
- A migration mechanism. No migration implementation is required until a
  second schema exists, but the version boundary must already be present.
- Preservation of the last valid in-memory snapshot when a live reload finds
  an invalid file.
- Diagnostics that identify the scope and property without echoing secrets.

### Could

- Advisory locking specifically for concurrent preferences-file editors. This
  is separate from the mandatory repository mutation lease for fix jobs.
- Automatic live reload of preference changes.
- Backup copies and explicit restore UI.
- Comment and formatting preservation across settings edits.
- Import/export of agent profiles with credentials stripped.
- Per-language or per-repository profile recommendations.

### Won't yet

- Storing provider credentials directly.
- Allowing repository preferences to register arbitrary executables or shell
  commands.
- Synchronizing preferences through a hosted Slopwatch account.

### Required property groups

The properties schema needs extension points for:

- agent profiles: runtime kind, adapter properties, auth reference, defaults;
- fix defaults: target, focus metrics, change scope, runtime profile, model,
  effort, delegation, limits and prompt-template reference;
- concurrency: maximum running agents, maximum verifiers, optional per-profile
  limits and retained-job count;
- validation: configured checks and required/optional policy;
- Git delivery: default mode, base/remote, branch template, commit policy;
- pull requests: publisher kind, draft policy, title/body templates;
- job retention: logs, worktrees and candidate cleanup policy.

### Settings information architecture

Settings gains four feature-owned entries while continuing to save through
the preferences application API:

- **Agents**: configured profiles and their current readiness;
- **Fix defaults**: goal, metrics, scope, validation, profile/model/effort,
  delegation, limits and prompt template;
- **Concurrency & retention**: agent/verifier limits, transcript limits and
  archive/cleanup policy;
- **Git & pull requests**: delivery default, remote/base policy, branch,
  commit and draft-PR templates and publisher readiness.

Agents first shows a table of stable profile name, runtime kind, transport
(`CLI` initially; future managed API/tool-loop), readiness, detected version
and default marker. Add/Edit opens an adapter-schema-driven form. Common
fields are profile name, runtime kind, executable selected from a resolved
path or explicit trusted absolute path, authentication reference, permission
policy and defaults. Adapter-specific options render only from that adapter's
typed schema; users never author a shell command line. Changing runtime kind
requires confirmation before incompatible fields are discarded.

**Test connection** runs the read-only probe and shows version, auth status,
capabilities, confinement guarantee and actionable diagnostics. It cannot run
an inference request. **Authorize** either launches a specifically supported,
trusted provider login flow after leaving the alternate screen, or shows the
exact external command the user must run; Slopwatch does not scrape arbitrary
interactive login output. Saving an unready profile is allowed as disabled,
but it cannot be selected for a job. Deleting a profile referenced by a
running/retained job is blocked; its pinned snapshot remains readable even if
the profile is later removed.

Every settings form distinguishes inherited and overridden values using
origin metadata, supports Reset to inherited, validates templates with a
preview, and uses expected preference revision on save. Returning from
settings restores the caller overlay and selection. Atomic file replacement
is supplied by the preferences subsystem rather than reimplemented in these
dialogs.

## Authentication and trust

CLI strategies normally reuse authentication owned by their provider. The
settings UI probes readiness and either suspends the alternate screen for a
trusted login flow or tells the user which external command to run. It does not
proxy an unknown interactive login conversation inside a normal modal.

Future API-backed runtimes resolve a keychain or environment reference at the
last responsible moment. Credentials are not placed in argv, prompts, job
journals, provider event payloads or repository-controlled process
environments.

Repository contents and analyzer evidence are untrusted input. Filesystem
permissions, sandboxing, change-scope checks and separation of publication
authority provide enforcement; prompt wording alone does not.

### Runtime threat model and eligibility

A Git worktree is isolation from other working files, not a security sandbox:
its `.git` indirection refers to shared repository metadata, repository files
can contain adversarial instructions, and an autonomous CLI may invoke tools
or use the network. A runtime is eligible to mutate a candidate only when its
probe and launcher can establish all of these properties:

- non-interactive execution with no approval or privilege-escalation path;
- writes confined to the candidate tree, with explicit writable/read-only/
  denied root sets and the Git common directory always denied;
- tool subprocess reads limited to the candidate, declared toolchain/runtime
  roots and a credential-free Git metadata projection; Slopwatch state,
  journals, preferences, unrelated workspaces/home files, SSH material,
  keychains and provider/publisher/Git-host credentials are denied;
- a bounded, declared network policy appropriate to the selected profile;
- a minimal allowlisted environment with control variables, shell startup,
  editor/pager, hook and unrelated credential variables removed;
- no publisher, Git-host or other agent-unnecessary credentials;
- bounded wall time, output, event count, actor count and child process tree;
- verified compatible executable identity/version, process-tree shutdown and
  crash-orphan containment with an unforgeable job/process lease.

The capability model represents the guarantee honestly:

```go
type WriteConfinement uint8

type RuntimeIsolation struct {
    Writes               WriteConfinement
    SensitiveReadsDenied bool
    TransportAuthIsolated bool
    CrashContainment     bool
}

const (
    AdvisoryOnly WriteConfinement = iota
    CandidateTreeEnforced
    CandidateTreeAndGitMetadataProtected
)
```

V1 mutating execution requires
`CandidateTreeAndGitMetadataProtected` and all three isolation booleans. A
prompt request or CLI flag with only advisory behavior is insufficient. If the
Codex CLI spike cannot prove this on a supported platform, the adapter may
remain probe/demo-only and Phase 2 has no real mutating runtime on that
platform. This is a release gate, not a warning the user can click through.

The process launcher builds an environment from a small platform-specific
allowlist, passes prompts through stdin/private files, and starts the runtime
in an OS containment mechanism. Provider transport may access only its
provider endpoint and narrowly brokered authentication; repository-controlled
tool subprocesses cannot inherit or read that authentication. A CLI/runtime is
ineligible when it cannot separate transport authority from its tool sandbox.
An OS parent-death facility or persistent supervisor must terminate and reap
all descendants after Slopwatch crashes and back that ownership with an
unforgeable job lease. Merely saving a PID or refusing recovery later is not
sufficient.

Validation executes under the equivalent read/write/denied roots, sanitized
environment, bounded output/time/process tree, default-deny network policy and
crash containment. It receives no provider or publisher credentials. Both
analysis and validation are bracketed by candidate fingerprints; unexpected
mutation invalidates their results. Publication runs later in a separate
process context with publisher credentials but without agent credentials,
agent tools, repository hooks or an editable candidate. Session/resume
references are treated as secrets: journals store an encrypted/keychain
reference or omit them, never raw provider tokens.

Transcripts and journals can contain source text and sensitive diagnostics.
Their directories are private (`0700` where supported), files are created
without following symlinks and with private mode (`0600` where supported), and
size/event/actor limits are enforced before persistence. Redaction removes
known configured secret literals and control sequences; documentation and UI
must not claim it can recognize every secret embedded in arbitrary source.

## Watcher and verification behavior

An agent can generate many intermediate filesystem events. The job service
records bounded changed-path/activity evidence and periodically reconciles the
actual Git diff, but does not feed candidate worktree changes into the main
dashboard watcher or reuse its analyzer. Verification is requested through
`AnalysisService` only after the runtime is quiescent, with optional deliberate
checkpoints between attempts.

Each verification gets a fresh analyzer rooted at the candidate analysis root
and a candidate-local cache policy. It captures a filesystem/content
fingerprint before analysis and rechecks it after analysis and configured
validation. Mutation invalidates the result. Cancellation terminates the
complete analyzer/validation process tree before releasing its verifier slot.
The dashboard may continue rescanning the original checkout independently;
those results never replace the job's pinned baseline or candidate result.

## Job journal and recovery

The journal is part of correctness, not an optional activity log. One service
controller is its only writer. `Submit` does not return an admitted job ID
until the admission record, reservation and complete pinned input snapshot are
durable. The snapshot contains the resolved non-secret preferences, prompt
template/version and compiled instructions, full scoring contract and metric
catalog identity—not only hashes that cannot reconstruct the decision later.

The durable protocol is:

- append versioned, length-delimited records with sequence number and checksum;
- record intent and expected job revision before every external side effect;
- flush records required to acknowledge admission, cancellation, destructive
  cleanup or publication (`fsync`, including the containing directory where
  required by the platform);
- periodically write a complete checkpoint to a private temporary file, flush
  it, atomically replace the old checkpoint, then compact only records covered
  by that checkpoint;
- on recovery, load the last valid checkpoint and replay valid records in
  order, ignoring only a final torn/incomplete record; corruption earlier in
  the stream is an actionable recovery error, never guessed through.

The journal records job/attempt/command IDs and idempotency receipts;
timestamps and state revisions; pinned repository/workspace identity and
target objects/hashes; resolved profile fingerprint and non-secret settings;
limits and validation policy; candidate ownership, diff and verification
fingerprints; cancellation/result races; and publication intent, commit, ref
and PR identities. Raw credentials, prompts containing resolved secrets and
raw resume/session tokens are prohibited.

Journal and transcript directories use the private/no-follow/size controls in
the threat model. The storage implementation is injectable and includes crash
tests at every intent/result boundary plus torn-write, duplicate-command and
checkpoint-compaction tests. These durability requirements are independent of
the preferences file's lower first-slice write priority: a mutable job cannot
be safely admitted without them.

On restart, an active phase becomes `interrupted`/`awaiting_action` until
reconciliation proves the candidate and any external side effects. Jobs are
never silently resumed, published, deleted or declared canceled. A saved PID
is not proof of process identity. Mutation is unavailable on a runtime/platform
pair unless the mandatory supervisor/parent-death mechanism and unforgeable
lease contract pass their crash tests. Recovery reconciles that lease before
offering any action. Publication recovery queries exact journaled object/ref/PR
identities before offering retry. Retained candidates are displayed even when
automatic recovery is unavailable.

## Proposed package ownership

Package names may follow repository conventions, but responsibilities and
dependency direction are normative:

| Slice | Owns | Must not own/import |
| --- | --- | --- |
| `internal/scoring` | metric catalog, weights, predicates and evaluation | TUI, cache, runtime, Git |
| analysis adapter | `AnalysisService`, conversion from analyzer/report/cache | agent, TUI presentation, Git |
| validator | trusted check registry, confined argv execution and results | shell text, provider/publisher auth, scoring decisions |
| `internal/agent` | runtime protocol, profiles, capabilities, registry, contract kit | analysis/cache, properties, Git delivery, TUI |
| adapter package per runtime | probe/execute transport and normalized events | scoring decisions, cache, publication |
| `internal/fixapp` | prepare/submit/commands, controller, scheduler, lifecycle, projections | provider-native parsing, preference files |
| candidate workspace | detached worktree ownership, paths, diff and scope | provider auth, PR APIs, TUI |
| job store | journal/checkpoint protocol and private transcripts | lifecycle policy, provider commands |
| delivery | commit/ref workflow and publication saga coordination | agent execution, TUI |
| publisher package | provider-specific PR create/query | editable runtime workspace, scoring |
| `follow` | Bubble Tea state, overlays, forms and rendering | cache files, provider protocols, Git commands |

Composition injects `AnalysisService`, `ValidationService`, preferences
resolver/store, runtime registry, candidate workspace, job store, confined
process launchers, delivery and publisher into `fixapp.Service`. Analysis,
validation, candidate workspace, delivery and publisher are separately
fakeable; tests do not need a real CLI, Git host or account.

## Delivery sequence

### Phase 0: contracts and preferences seam

- Integrate the forthcoming preferences API through a narrow injected port.
- Add the required property groups and trust-boundary validation.
- Provide an in-memory preferences implementation for tests.
- Extract shared metric definitions/evaluation into `internal/scoring` and
  implement the analysis baseline/verification port.
- Define repository/candidate identity, `RepoPath`, `FixSpec`, frozen scoring
  contract, lifecycle outcomes and allowed-action projection.
- Define the deep runtime interface, typed capabilities and strategy registry.
- Define the validation port, trusted check representation and common
  confinement/process contract shared with runtime tools.
- Spike Codex CLI non-interactive protocol, OS-enforced candidate/Git-metadata
  confinement, sanitized environment and process-tree cancellation on every
  intended platform. Record evidence; do not relax the release gate on failure.
- Specify the durable journal record/checkpoint formats and crash matrix.
- Resolve or retire the currently parsed-but-unused `--config` and `--backend`
  flags so they do not conflict with the new configuration story.

### Phase 1: domain and fake-runtime vertical slice

- Add an analysis/evidence provider that supplies typed snapshots without
  exposing the disk cache.
- Add prompt compilation and deterministic golden tests.
- Add fresh goal evaluation and completeness/regression tests using a frozen
  scoring policy.
- Add fake validation and prove scoring/validation outcomes remain independent.
- Add `fixapp.Service`, single-writer controller, fair schedulers, fake runtime,
  fake analysis/workspace/delivery and durable in-memory/file job stores.
- Prove level-triggered snapshots, command idempotency, cancel races, journal
  recovery and concurrent isolation under the race detector.
- Replace modal booleans with `MainView` and the typed overlay stack.
- Implement `x`, the form, advanced guidance, Files/Agents switching, expanded
  job files, the per-job monitor, individual cancellation and verified result
  views against fakes.
- Implement settings screens against the injected preference store, including
  asynchronous profile probes and caller-preserving navigation.

### Phase 2: safe local candidate

- Add canonical Git discovery, cross-process repository lease and private
  detached worktree ownership/cleanup; create no branch yet.
- Add Codex CLI only on platforms where the Phase 0 confinement/protocol spike
  passed, using the shared adapter contract suite and fake executable.
- Add structured progress parsing, bounded private logs, sanitized environment
  and process-tree cancellation.
- Add the confined executable-plus-argv validator and its fake-executable,
  command-injection, environment, network and crash-containment contract tests.
- Add fresh mutation-detecting verification in the candidate analysis root.
- Run at least two isolated fake/real-adapter jobs concurrently while queuing
  additional work at the configured limit.
- Show retained candidates and diffs; do not commit, branch, push or open a PR.

### Phase 3: additional runtime strategies

- Add at least one more explicit CLI adapter only after it meets the same
  confinement and process-control release gate.
- Add adapter contract tests with fake executables.
- Enable capability-driven model, effort and delegation controls.
- Add readiness/authentication management in Settings -> Agents.

### Phase 4: publication

- Add the journaled commit/exact-ref push saga and GitHub CLI draft-PR publisher.
- Enable the delivery/publisher portions of Settings -> Git & pull requests.
- Add branch, commit and PR templates with preview and collision handling.
- Add lost-response reconciliation, multi-PR conflict visibility and explicit
  archive/discard/reconciled-cleanup actions.

### Phase 5: expansion

- Add multi-select UI using the existing multi-target domain.
- Add bounded retry/resume loops using verification feedback.
- Add usage/cost budgets where runtimes report them.
- Add a controlled tool-loop runtime before supporting raw model APIs.

## Minimum acceptance criteria

- `x` snapshots the intended target even if the table reranks.
- Other active jobs never prevent the user from configuring and submitting a
  non-overlapping fix.
- The Agents view presents every job, goal and target and supports simultaneous
  expansion of compact verified file metrics.
- Runtime packages have no dependency on analysis caches or TUI/report models.
- Baseline/verification use the typed analysis port, and incomplete or
  mutation-invalidated results can never be marked compliant.
- Adding an explicit runtime strategy requires no provider-specific change in
  the follow package.
- Unsupported model, effort or delegation combinations cannot be submitted.
- Prompt output is deterministic for a versioned task and invalid templates
  fail during preflight.
- No built-in runtime invokes a shell, exposes secrets in argv/logs, or launches
  without enforced candidate-tree and Git-metadata protection.
- Cancellation terminates the runtime and stale events are ignored.
- Cancellation is job-scoped; other running and queued jobs remain unchanged.
- Out-of-policy changes remain inspectable but block publication.
- Success requires a fresh analysis against the frozen scoring contract.
- Watcher bursts cannot make an intermediate file state the final result.
- Concurrent jobs have distinct detached worktrees; branch/Git administrative
  operations are serialized without serializing agent execution.
- Admission, cancellation, cleanup and publication survive crash-point tests
  without duplicate side effects, lost reservations or unsafe deletion.
- Multiple published PR jobs remain independently inspectable and overlapping
  exact diffs produce durable, hash-bound conflict warnings.
- Dirty repositories, detached HEAD, branch collisions, missing runtime/auth,
  validation failure and PR failure produce actionable, recoverable states.
- The exact responsive-size and overlay-update UI matrix passes without hidden
  controls or stale-row actions, and with no destructive action on the resize
  screen except the explicitly confirmed compact cancel-all-and-quit flow.
- TUI tests use fake services, adapter tests use fake executables, and Git tests
  use temporary repositories; no automated test requires a provider account.

## Resolved review decisions and release gates

No product choice from the senior review remains an implementation blocker:

- Default target is `SCORE <= 100` after preference/CLI precedence.
- Default scope is `targets-and-tests`: declared target files plus files the
  existing unit planner classifies as tests in the same analysis unit(s).
  Other production/config/generated files remain visible but violate scope.
- A stable candidate always reaches Review even when noncompliant, validation
  failed or validation is absent. Candidate-only Keep remains available.
  Default PR publication requires compliant complete scoring, clean scope, at
  least one configured validation check, and all required checks passing.
- Phase 2 requires the completely clean baseline defined above. A clean
  detached HEAD is allowed for candidate-only work; PR mode additionally
  requires an explicit resolvable base branch.
- Initial real runtime is Codex CLI, but only where the protocol/confinement
  spike meets the common contract. Failure blocks that adapter/platform rather
  than weakening the boundary.
- Initial publisher is GitHub CLI; all created PRs are draft.
- Default capacity is two agent attempts and one verifier, configurable in
  preferences and visible as the queue reason.
- Quit cancels and joins all jobs. Detached/background jobs are out of v1.
- Completed jobs appear under All until archived. Worktrees, branches and
  transcripts have no time-based auto-deletion in v1; only explicit Discard or
  later reconciled cleanup removes proven job-owned artifacts.

Two Phase 0 feasibility gates remain intentionally empirical rather than
ambiguous: the incoming preferences API must satisfy the resolver/store seam,
and each supported OS/runtime pair must prove write confinement and process
control. A failed gate yields an actionable unsupported capability and a
revised implementation proposal; it never authorizes a direct cache/property
dependency or advisory-only mutating runtime.

### Current implementation gate evidence (2026-08-25)

The first Codex CLI adapter and executable conformance sentinel are implemented,
but mutating Codex execution is intentionally unavailable on the audited macOS
host. With Codex CLI 0.149.1, the exact generated permission profile denied the
configured authentication/sensitive root, but the sentinel could still read
the linked-worktree Git metadata and write outside the candidate (including
the platform temporary roots). The self-reexec supervisor proved cancellation,
timeout, normal descendant cleanup and parent-crash cleanup for one process
group, but a process group is not confinement against a child creating a new
session. Production therefore reports failed measured gates and keeps
`CrashContainment=false`; `Submit` rejects mutation. Validation is likewise
fail-closed until an equivalent confined executor is available. This is an
empirical platform release gate, not a user-overridable warning and not a claim
that the end-to-end Fix action can currently mutate files on that host.
