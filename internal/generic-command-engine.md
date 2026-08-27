# Generic command engine: reconstruction design

## Status and intent

Slopmochi previously contained a product-specific command runner tied to fix jobs. It was removed because the product did not yet have clear user journeys for defining, selecting, and interpreting local commands. Presenting that runner as “validation” also implied a partial CI system without the workflow, discoverability, or value needed to justify one.

This note preserves the reusable engineering ideas so a future, generic command engine can be rebuilt when concrete use-cases exist. It is a design record, not a dormant feature specification. Nothing in the running application should depend on it.

## Core boundary

The engine should accept structured commands from a trusted application-owned source and return structured observations. It must not know about fix jobs, scores, agents, pull requests, settings dialogs, or CI concepts.

```go
type Command struct {
    ID               string
    Executable       string
    Arguments        []string
    WorkingDirectory string
    Environment      []string
    Stdin            []byte
    Limits           Limits
}

type Limits struct {
    WallTime         time.Duration
    TerminationGrace time.Duration
    MaxStdoutBytes   int64
    MaxStderrBytes   int64
}

type Observation struct {
    StartedAt        time.Time
    FinishedAt       time.Time
    ExitCode         int
    Stdout           []byte
    Stderr           []byte
    StdoutTruncated  bool
    StderrTruncated  bool
    TimedOut         bool
    Canceled         bool
}

type Executor interface {
    Readiness(context.Context) Capability
    Execute(context.Context, Workspace, Command, Observer) (Observation, error)
}
```

`Observer` is an optional streaming callback for bounded output chunks and lifecycle events. Cancellation is driven by `context.Context`; elapsed time alone must not imply inactivity. `Execute` owns the full child-process lifecycle and must not return while descendants it owns are still running.

The caller owns all higher-level policy: where command definitions come from, whether a non-zero exit is acceptable, sequencing or parallelism, retry decisions, presentation, persistence, and how results affect another workflow.

## Definitions and trust

Commands are executable-plus-argument records, never shell strings. The engine must invoke the executable directly and must not interpret a shell language, interpolate repository-controlled variables, or inherit the host environment implicitly.

A future caller should distinguish two sources explicitly:

- trusted definitions: application or user configuration permitted to name executables, arguments, environment, and limits;
- untrusted workspace data: files supplied as input to a command but never allowed to redefine the command or its execution policy.

The caller passes the resolved command to the engine through the API. The engine must not reach into preferences, caches, repository files, or saved job state to discover configuration.

Validation of a command should reject empty executable paths, NUL/newline-bearing environment entries, invalid working directories, and nonsensical limits. Whether executables must be absolute or may be resolved through an explicit PATH is a use-case policy and should not be hard-coded before those use-cases are known.

## Workspace abstraction

```go
type Workspace struct {
    Root          string
    GitCommonDir  string
    SensitiveRoots []string
    Access        AccessMode
}

type AccessMode int

const (
    ReadOnly AccessMode = iota
    ReadWrite
    IsolatedCopy
)
```

The workspace is supplied by the caller. The engine canonicalizes paths, rejects working-directory escapes, and hands a fully resolved workspace to an execution backend. Git metadata is ordinary policy input, not an assumed requirement; non-Git directories must work.

`IsolatedCopy` is useful when commands may generate files but the source must remain unchanged. A copy implementation should:

1. walk beneath an already-open canonical root;
2. reject symlinks and non-regular files unless a future use-case defines safe semantics;
3. enforce caller-visible limits for file count, directory count, path bytes, per-file bytes, and total bytes;
4. exclude or mask VCS metadata when requested;
5. detect files that change during copying;
6. create the destination with private permissions.

These are backend capabilities. They must not become arbitrary mandatory product limits: expose the selected backend’s capabilities and let the calling use-case choose appropriate policy.

## Execution backends

Keep orchestration behind a narrow strategy interface. A direct local-process backend is the simplest baseline. Container, remote, sandbox, or operating-system-specific backends can be added independently when a use-case requires them.

Each backend reports capability rather than forcing a dependency:

```go
type Capability struct {
    Available          bool
    Backend            string
    ReadOnlyWorkspace  bool
    WritableCopy       bool
    NetworkControl     bool
    DescendantCleanup  bool
    Diagnostic         string
}
```

The application continues to start and operate when an optional backend is unavailable. Readiness checks must be cheap enough to run on demand, cached only by the owner that understands staleness, and accompanied by an actionable diagnostic.

### Local process backend

Reuse or extract the active `go/internal/isolation.Runner` primitive rather than
building a second process supervisor. It already provides direct argv,
explicit directory/environment/stdin, independently bounded stdout/stderr,
streamed stdout, process-group cancellation, escalation, draining, and reaping.
The command engine should adapt that low-level primitive to its own value types;
the primitive must remain independent of fix jobs and other product workflows.

Start the executable without a shell, with explicit argv, directory, environment, stdin, and output pipes. On Unix, place the child in its own process group. On cancellation or wall-time expiry:

1. send a graceful termination signal to the owned group;
2. continue draining bounded output while waiting for the configured grace period;
3. kill the owned group if it remains alive;
4. wait/reap the original process;
5. return an observation distinguishing cancellation from timeout and ordinary exit.

Other platforms need an equivalent ownership primitive rather than pretending process-group semantics are portable.

### Optional container backend

If a future use-case needs container confinement, implement it as a separately selectable adapter. Do not make Docker, a particular image, or container configuration a Slopmochi startup dependency.

The former design used an immutable, digest-pinned image and a trusted PID 1 supervisor. The host created a container with direct argv and these defenses:

- no image pull during execution;
- read-only container filesystem;
- no network;
- all Linux capabilities dropped and `no-new-privileges` enabled;
- non-root numeric UID/GID;
- explicit PID, memory, CPU, open-file, generated-file, temporary-space, and workspace limits;
- source mounted read-only, VCS metadata masked, and a bounded writable copy created on tmpfs;
- a small JSON request envelope mounted read-only;
- exact container ownership labels for reconciliation and cleanup.

The supervisor decoded a versioned request with unknown fields rejected, copied the workspace within limits, launched the command as its child, and treated loss of the host attach/control stream as cancellation. It terminated and reaped the entire child group before exiting.

Before declaring itself available, that backend verified the pinned image, verified configured executable paths inside the image, reconciled orphaned containers belonging to the same installation under an exclusive lease, and ran an empirical escape probe to prove a detached descendant could not survive supervisor/container exit. Any failed proof made only that backend unavailable.

This is deliberately a reference for reconstruction, not a requirement that a future engine use containers.

## Output and observability

Capture stdout and stderr independently with explicit byte limits. Once a stream reaches its limit, mark it truncated and keep draining/discarding so the child cannot block on a full pipe. Preserve raw bytes in the engine; text decoding and rendering belong to the caller.

Lifecycle events should be small and stable:

- queued (only if a scheduler exists above the engine);
- starting;
- running, optionally with bounded output chunks;
- terminating;
- finished with the final observation.

The engine should expose timestamps and the reason execution ended. It should not invent statuses such as pass/fail, compliant/noncompliant, review, retry, or publish; those are use-case interpretations.

## Safety invariants

- No implicit shell.
- No implicit host environment.
- No command discovery from the workspace.
- No direct access to application caches or preferences.
- Canonical paths and containment checks occur before launch.
- Cancellation targets only resources positively owned by the execution instance.
- Output is bounded without deadlocking the child.
- Cleanup is idempotent and exact; never select resources by a broad or ambiguous name.
- Optional confinement failure degrades that backend, not the application.
- Limits and timeouts are visible policy supplied by the caller; there are no silent product-wide ceilings.

## Suggested package structure

```text
internal/commandengine/
    command.go          # value types and Executor interface
    paths.go            # canonicalization and containment
    local/
        executor.go     # adapts the retained isolation.Runner primitive
    copyworkspace/      # add only when IsolatedCopy has a real consumer
    container/          # add only when a container use-case is approved
```

Dependencies point inward: product workflows depend on `commandengine.Executor`; adapters depend on command-engine value types; the engine never imports product workflow, UI, preferences, or persistence packages.

## Reconstruction tests

The minimum effective suite should cover behavior, not a second fake implementation:

- argv is passed literally, including shell metacharacters;
- environment is explicit and host-only values do not leak;
- working-directory escape is rejected;
- stdout and stderr are captured and independently truncated without deadlock;
- normal exit, non-zero exit, caller cancellation, and wall-time expiry are distinct;
- cancellation removes owned descendants;
- repeated cleanup is safe;
- unavailable optional backends return actionable readiness information without affecting application startup;
- workspace-copy limits and special-file rejection work at boundaries;
- a backend contract suite runs against every real adapter that can run in the current environment.

Container-specific tests, if that adapter returns, should verify command construction, exact-label cleanup, cancel/kill/wait ordering, unknown request-field rejection, immutable executable probing, orphan reconciliation, and the descendant escape probe. Environment-dependent integration tests must skip clearly when the backend is absent; ordinary Slopmochi build, install, startup, and tests must never require Docker.

## Decisions deferred until real use-cases exist

- who authors command definitions and where they live;
- local, repository, organization, or installation scope;
- whether PATH lookup is allowed;
- read-only, in-place, or copied workspace semantics;
- network policy;
- sequencing, concurrency, retries, and caching;
- persistence and retention;
- UI terminology and result interpretation;
- which isolation backends are worth maintaining.

Those decisions belong to the first concrete consumer. The engine should remain small until that consumer makes the required behavior explicit.
