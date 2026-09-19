# Connected confirmation source holdout

Source-only selection from live source and production callers. Expectations fixed without running scoring or inspecting candidate ratings. Prior exposure and coverage gaps are disclosed.

## Coverage

Two Go and two Rust cases provide fresh high/low comparisons. TypeScript provides mixed/low only: inspected source does not support a fresh high label. Higher SHALLOW represents caller burden relative to hidden responsibility, not code quality or implementation size.

| Case | Language | Band | Range |
| --- | --- | --- | --- |
| go-condition-variable | go | high | 65–100 |
| go-once-initializer | go | low | 0–25 |
| typescript-finalize-coordinator | typescript | mixed | 26–64 |
| typescript-recent-log-buffer | typescript | low | 0–25 |
| rust-maybe-uninit | rust | high | 65–100 |
| rust-once-lock | rust | low | 0–25 |

## go-condition-variable

Hidden work: Owns runtime waiter notification, atomic unlock/sleep/relock and copy-use detection.

Caller obligations: Caller owns the actual predicate, its state changes, the matching Locker, lock/unlock bracketing, repeated predicate tests around Wait, and choosing Signal/Broadcast at the right transitions. A wakeup does not establish the predicate.

database/sql/closemu.go RLock and Lock hold m.mu and retry atomic state predicates around Wait; RUnlock and Unlock decide when to Broadcast. The implementation hides a valuable waiting primitive but leaves a coupled synchronization protocol outside; high is justified by concrete caller sequencing, not operation count.

## go-once-initializer

Hidden work: Serializes competing initialization attempts, publishes completion only after callback finishes, guarantees later callers wait, and releases locking on panic.

Caller obligations: Caller supplies one callback and scopes one Once per initialization, avoids copying the Once after use and recursive self-initialization. No explicit locking, completion flag, wake or cleanup protocol is required at the caller.

net/textproto/reader.go calls commonHeaderOnce.Do(initCommonHeader), then reads commonHeader. Once hides the synchronization and publication duties that make this ordering safe.

## typescript-finalize-coordinator

Hidden work: Generates attempt IDs, stores an intent/promise pair, validates matching attempts, rejects stale and duplicate delivery, and resolves only the first matching result.

Caller obligations: Caller must register before waiting, serve intent and submit externally validated results through separate endpoints, race completion against timeout/pod loss, and abort in finally. Replacement leaves an old awaiter to its own timeout.

runtime/k8s/finalize.ts registers, constructs timeout and pod-probe races, awaits pending.result and aborts in finally; server/handlers/runs/finalize.ts validates the payload and calls peekIntent/submit. This is a substantive correlation abstraction with residual lifecycle obligations: mixed, not high.

## typescript-recent-log-buffer

Hidden work: Installs browser error/rejection/fetch observation once, normalizes and bounds captured strings, evicts oldest entries, preserves original console/fetch calls, and returns a defensive array copy.

Caller obligations: Caller imports the module and asks for recentLogs; report.ts applies its own additional redaction. It does not register handlers, cap entries or manage the buffer.

lib/report/report.ts imports recentLogs and consumes recentLogs().map(scrub) in its report snapshot. The getter is small because event collection and bounding are hidden at module scope; this is executable behavior, not a passive carrier.

## rust-maybe-uninit

Hidden work: Provides storage preserving layout without automatic T destruction, writes values and exposes pointers/conversions; slice initialization helpers additionally guard partial initialization on panic.

Caller obligations: Caller must prove initialization before assume_init/ref/read/drop, track ownership after reads, avoid double destruction and leaks, obey aliasing and validity requirements, and manage transitions independently. The storage does not maintain an initialized-state flag or own automatic cleanup of T.

std/sync/once_lock.rs separately tracks completion, writes MaybeUninit only after successful initialization, guards reads with initialized checks, resets completion before assume_init_read in take, and conditionally assume_init_drop in Drop. These are actual duties outside MaybeUninit. Slice helpers deserve real credit but do not remove the core unsafe ownership protocol.

## rust-once-lock

Hidden work: Owns initialized-state checks, synchronized initialization and publication, retries after failed initialization, safe references, take/reset transitions and conditional value destruction.

Caller obligations: Caller provides a value or initialization closure, chooses get/wait/get_or_init semantics and avoids recursive initialization. Safe callers do not track MaybeUninit validity or manually destroy a successfully stored value.

std/io/stdio.rs stdin scopes a static OnceLock and uses get_or_init to construct its shared buffered reader. This one call hides the state proof, initialization synchronization and ownership obligations demonstrated internally against MaybeUninit.

## Relationships

- go-condition-variable exceeds go-once-initializer by at least 25: Caller-managed predicate/locking/wakeup protocol is materially shallower than owned one-time initialization synchronization.
- rust-maybe-uninit exceeds rust-once-lock by at least 25: OnceLock demonstrably owns the validity tracking, publication and conditional destruction that MaybeUninit requires its caller to implement.
- typescript-finalize-coordinator exceeds typescript-recent-log-buffer by at least 5: External register/wait/timeout/abort and callback correlation obligations exceed a module-owned bounded error buffer read.

## Limits

- Two cases per language: Go high/low, Rust high/low, TypeScript mixed/low. No new TypeScript-high claim. Original source pool did not suffice for balanced high-band coverage; expansion to installed Go/Rust source and local career-ops/ap sources still did not yield a defensible fresh TS high within the bounded review.
- Existing connected-high corpus and its first evaluation remain unchanged. This confirmation corpus cannot erase earlier Go/TypeScript high misses or substitute for their regression criteria.
- Purposive small sample, not random or enterprise representativeness. System primitives are intentionally low-level APIs; high SHALLOW here describes retained caller responsibility, not an assertion that the primitives should be redesigned.
- Selection did not invoke any scorer or inspect candidate scores, grading outputs or sourceestimate implementation. Prior-task exposure: structural.ts lines 875-905, beginning of legacy feature aggregation during caller tracing. The selector was told earlier connected-high Go/TS cases missed high; candidate-specific scores were not supplied.
- MaybeUninit contains meaningful slice-initialization guards and OnceLock contains compiler-internal/unstable syntax. Exact installed source is preserved; unsupported syntax, missing numeric ratings and grading failures must remain visible, with no fixture edits to manufacture a pass.
- Snapshots omit full build workspaces, runtime internals and third-party dependencies. Witness files are not scored inputs; incomplete evidence must be reported honestly.
- After first evaluation all expectations, relations and hashes remain frozen. Tuning against this corpus consumes its confirmation status; retain failures unchanged.
