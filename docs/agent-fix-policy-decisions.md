# Agent-assisted Fix policy decisions

Status: **owner direction incorporated**.

This ledger distinguishes non-disableable correctness/security invariants from
operational product policy. Slopwatch must not silently impose operational
limits. Every operational constraint has a named Settings control, displays its
effective value and source, and explains what happens when it is reached.

## Correctness and security invariants

These are not settings because disabling one would make the operation
incorrect or cross an authority boundary:

- Agent adapters receive a typed request, immutable analysis evidence, and a
  candidate capability. They never read Slopwatch caches, preference files,
  Git publication state, credentials, or arbitrary ambient paths.
- Each mutating job owns an isolated worktree and durable identity. Target
  reservation is atomic; cancellation is per job.
- Agent completion is not compliance evidence. Slopwatch independently
  inventories the diff, re-runs scoring, and labels the result.
- Provider execution, events, cancellation, and recovery remain behind the
  cohesive provider-neutral `agent.Strategy` interface. Adapter-only settings
  come from adapter-owned schemas.
- Responses API tools have no shell, Git, process, or arbitrary-network access.
  Candidate file access is rooted and write-scoped. Authentication references
  are resolved only at execution time.
- Publication never force-pushes, overwrites an existing ref, runs repository
  hooks, auto-merges, or silently discards a candidate. Exact remote, commit,
  branch, and pull-request identity are pinned and revalidated.
- A checked-out repository cannot introduce commands, credentials, provider
  identities, publication destinations, or broader authority. It may only
  select trusted definitions and narrow user-owned policy.
- Executable paths and confinement backends must satisfy the same capability
  contract before they may mutate a candidate. This is capability gating, not
  a preference for one provider or container product.
- Memory, file, protocol, and transcript operations are bounded by their
  effective configured values so model/provider output cannot allocate without
  accounting. There is no hidden product ceiling above those visible values.
- Fixed wire-envelope and individual display-field shape bounds are protocol
  invariants, not work budgets: oversized display text is visibly elided and
  an oversized malformed control envelope is rejected. They do not stop an
  otherwise active agent based on time, turns, attempts, files, or cost.

## Accepted operational policy

| Area | Accepted behaviour | Settings surface and consequence |
| --- | --- | --- |
| OpenAI authentication | Codex with provider-owned sign-in is the recommended built-in default; ChatGPT sign-in is recommended, while Codex also reports API-key-backed sessions truthfully. Direct Responses API access remains a separately selected API-key profile; there is no automatic fallback between routes. | Settings › Agents labels the authentication/billing route, reports sanitized readiness, marks `DEFAULT`, and uses `D` to change the default. Existing explicit user selections are preserved. |
| Agent duration | No Slopwatch wall-clock timeout for an active fix attempt. Liveness comes from progress/activity events and explicit per-job cancellation. | No misleading timeout control. Provider transport budgets, if an adapter supports them, must be adapter settings and identify exactly what they time. |
| Attempts and retry | One initial attempt. Retry is an explicit job action that creates a new durable attempt with prior verified evidence. No attempt maximum and no automatic retry. | Job history shows attempt ordinals and each retry remains available in retryable/review states. |
| Concurrent work | Defaults remain 2 running agents and 1 verifier. Additional jobs are accepted and visibly queued. No compiled hard maximum. | Settings › Concurrency & retention; effective values and queueing consequence are shown. |
| Retention | Defaults remain 100 retained jobs and an exact 1 MiB per-job JSON-encoded transcript-entry budget. No compiled hard minimum or maximum. | Settings › Concurrency & retention; saving a smaller budget trims oldest entries immediately, and reaching the budget is a visible job outcome, never silent truncation presented as success. |
| Delegated actors | The maximum distinct actors represented in a job transcript is configurable; default 32. | Settings › Concurrency & retention › Actors per job. Provider delegation modes remain capability-advertised. |
| Candidate source preview | Preview bytes and rendered lines are positive, user-configured per-job values with defaults of 4 MiB and 5,000 lines. No compiled preview ceiling. | Settings › Concurrency & retention; truncated previews say so explicitly. |
| Responses work budgets | Model turns, tool calls, output tokens, and input-token checks default to `0`, meaning no Slopwatch-imposed budget/provider default as labelled. | Settings › Agents › profile. Each adapter-owned field states its `0` semantics. |
| Responses resource budgets | Response, request, local-context, tool-result, read, write, list, and summary sizes have visible defaults and no hidden maximum. | Settings › Agents › profile. Invalid relationships are rejected with the exact fields named. |
| Validation policy | PR validation is optional by default and may be required by user/installation policy. Selecting a plan means it must be runnable and pass before publication. | Settings › Git & pull requests › PR validation, plus Settings › Validation plan selection. |
| Validation limits | Check required/optional status, timeout and output bytes are editable; executable and arguments remain part of a trusted plan definition. Candidate file, directory, path-byte, per-file-byte and total-byte ceilings are user-configured and shared by confined copy plus before/after fingerprinting. No compiled product maximum. | Settings › Validation shows every effective constraint, source and failure consequence. |
| Validation container resources | Process, memory, CPU, `/tmp`, workspace tmpfs, open-file, generated-file, stop, Docker-control, sentinel and crash-probe quantities are finite, positive user-owned policy. Repository config cannot author or widen them. Workspace tmpfs must exceed admitted total bytes; generated-file bytes must cover admitted max-file bytes; control timeout must exceed stop timeout. | Settings › Validation labels active-check policy separately from confinement lifecycle/readiness policy and explains that tmpfs consumes container memory. |
| PR state | Draft is the default, not a mandate. Ready-for-review creation is supported. | Settings › Git & pull requests › Initial PR state. |
| PR base | The user chooses the base branch. Its exact remote existence is checked at publication time, so an unavailable base does not prevent the refactor itself. | Settings › Git & pull requests › Base branch explains publication-time validation. |
| Branch naming | `{job-short-id}` is available in the suggested default but is not mandatory. Organisation-specific templates are accepted. Existing remote/local refs produce an actionable collision error. | Settings › Git & pull requests › Branch template shows a preview and collision behaviour. |
| Delivery workflow | Candidate-only, branch, and pull-request modes are user-selectable. GitHub CLI is the initial publisher adapter, not a core restriction. | Settings › Git & pull requests; the fix dialog shows the exact selected mode and branch before Run. |
| Git and publisher command resources | Candidate-worktree Git, delivery Git and publisher commands have no Slopwatch wall-clock timeout; explicit job cancellation remains authoritative. Captured stdout/stderr has one positive user-configured byte budget pinned into each prepared job. | Settings › Git & pull requests › Command output bytes. |
| Cancellation escalation | Provider cancellation grace is adapter-configurable (Codex exposes it in Settings › Agents). Small supervisor/control waits that begin only after cancellation are fixed security invariants: they cannot stop live work and prevent a wedged descendant from blocking cleanup forever. | Adapter-owned grace is visible with its consequence; fixed cleanup invariants are named in code and documentation rather than presented as job timeouts. |
| Workspace admission | A clean source worktree is required for v1 because dirty-state capture/restoration has not yet been implemented safely. | Preflight states this requirement and remediation explicitly. This remains a documented v1 capability gap, not an unexplained timeout/limit. |
| Change scope | `targets-only`, `targets-and-tests`, and `repository`; default `targets-and-tests`. | Settings › Fix defaults and the per-fix dialog show the selected scope. Repository-wide mutation remains an explicit choice. |

## Settings presentation contract

Every constraint control must display:

1. a human label rather than a storage key;
2. the effective value, including units;
3. its origin (`built_in`, `user`, `repository`, CLI, or session);
4. the operational consequence when reached;
5. whether `0` means unlimited, provider default, disabled, or is invalid.

Adapter settings are described by the adapter and rendered generically by the
Agents settings UI. This keeps the agent framework deep and cohesive while
allowing future Claude, Grok, AG-UI, or other adapters to expose different
controls without changing orchestration or cache boundaries.

## Deliberately deferred rather than prohibited

- Multi-file selection in the Files view; domain and job APIs already use
  target slices.
- Additional agent and publisher adapters.
- Provider-native delegation modes where a runtime can prove and report them.
- Dirty-worktree capture/restoration and automatic cleanup after a PR closes.

These follow-ups must not make the coordinator, cache, preferences, or TUI
depend on a provider-specific API.
