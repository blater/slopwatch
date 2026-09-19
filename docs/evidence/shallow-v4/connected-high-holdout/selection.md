# Connected high real-source holdout selection

Frozen before any candidate score execution or score inspection. This selector is separate from the implementing agent but previously reviewed scorer source. The six cases and three ordering relations are now fixed.

Read repository AGENTS.md, P0 directive and graded-independent-review.md rubric. The latter contains historical scores for other sources; those were encountered and are disclosed. Candidate scores remain unseen. No claim of complete scorer-source blindness is made.

## Selection rule

Select actual caller-owned lifecycle/protocol or unsafe live-state obligations, with concrete callers, and distinguish bookkeeping/capability discovery from ownership of the operation sequence. Contrast with cohesive boundaries that discharge substantive creation, publication or unwind/structural repair duties. Do not label by brevity, method counts, syntax, complexity, or imagined callee behavior. High ratings here are not software-quality judgments.

## go-response-controller

Frozen band: 65–100 (high).

Hidden duties: Unwraps writer layers, discovers optional capabilities, normalizes unsupported-operation errors and prefers error-returning Flush. These are real dispatch duties, but it does not own an HTTP exchange or hijacked connection.

Retained obligations: Caller owns ServeHTTP lifetime, flush boundaries, deadline timing and error policy, full-duplex activation and I/O sequencing. After hijack, caller owns response framing, buffered flush, bidirectional copy, cancellation and closure.

Caller evidence: Production httputil/reverseproxy.go copyResponse/maxLatencyWriter schedules and stops flush timers; handleUpgradeResponse validates upgrades, calls Hijack, then coordinates cancellation, defer conn.Close, response serialization, buffer flush and two copy directions. responsecontroller_test.go demonstrates sticky deadline semantics and manual read/write/flush sequencing. High follows retained cross-call protocol, not API width; capability discovery does not remove those duties.

Provenance: Go go1.27.1 darwin/arm64, Homebrew Cellar go/1.27.1; VERSION timestamp 2026-08-28T16:20:06Z

Snapshot SHA-256: `7276f45a5fed50dad368b75afe916b3e4dc45bca2e23e438f1095b0fdc6f4ad6`.

## go-tempfile

Frozen band: 0–25 (low).

Hidden duties: Owns default directory selection, separator validation, wildcard splitting, random paths, restrictive modes, exclusive creation and bounded collision retries.

Retained obligations: Caller supplies directory/pattern, handles errors, uses results and closes/removes successfully created resources. Successful lifetime is not credited as automatic cleanup.

Caller evidence: os/tempfile_test.go TestCreateTempPattern and TestMkdirTemp show one create call followed by caller Close/Remove. Callers do not implement random naming, exclusive creation or retry loops. Compact cohesive creation hides substantive policy despite retained lifetime.

Provenance: Go go1.27.1 darwin/arm64, Homebrew Cellar go/1.27.1; VERSION timestamp 2026-08-28T16:20:06Z

Snapshot SHA-256: `a337c2a6dc2e4368b2dc0015ea7c0b935b68b14885a0771a6cbe27e588c2cd16`.

## typescript-tracker-write-registry

Frozen band: 65–100 (high).

Hidden duties: Generates monotone tokens, maintains private active-writer accounting, supports idempotent release and computes whether any token remains. It owns bookkeeping, not exclusion of tracker operations or worker lifetime.

Retained obligations: Writers decide whether to acquire, retain tokens, release on completion/error/cancel and align release with actual mutation completion. Deleters independently query and refuse work and maintain their own deleting flag. No guarded callback, waiting or automatic cleanup is provided.

Caller evidence: api/run/route.ts acquires before spawn and manually releases in close() and cancel(); api/tracker/delete/route.ts checks isTrackerWriting, returns 409 and maintains deleting with try/finally. Safety-relevant lifecycle sequencing remains spread across actual callers. High is not inferred from method names or a short implementation.

Provenance: career-ops git HEAD 1ea5ee6379a90dfb1bec17b01aa538a624953848; exact working-tree bytes

Snapshot SHA-256: `7df5129cd1e1dc3c702fbb210471f057ad7880eb941bf401d0f3433d4fb6e084`.

## typescript-safe-write

Frozen band: 0–25 (low).

Hidden duties: Creates parent directories, chooses a unique same-directory temporary path, writes complete content before rename and offers backup-before-publication orchestration.

Retained obligations: Caller chooses path/content, validates domain data and handles failures. Backup is best-effort; this does not guarantee fsync durability, temporary-file cleanup or transactional backup success.

Caller evidence: api/cv/route.ts validates payload and calls atomicWriteWithBackup once without orchestrating mkdir/temp names/write/rename or backup ordering. This is cohesive publication responsibility, not a claim every I/O failure is safely repaired.

Provenance: career-ops git HEAD 1ea5ee6379a90dfb1bec17b01aa538a624953848; exact working-tree bytes

Snapshot SHA-256: `cd5c58ff0083d14cbd14ffab929c76d99140e300a986acd0bbc68e077f019763`.

## rust-manually-drop

Frozen band: 65–100 (high).

Hidden duties: Suppresses automatic destruction and supplies wrapping, extraction, raw take/drop and value trait forwarding. into_inner consumes the wrapper, but unsafe take/drop do not track whether its contents remain live.

Retained obligations: Caller upholds exactly-once destruction, no subsequent use or safe exposure after take/drop, drop ordering and unwind safety, and external live-state invariants. Safety documentation states duties; it does not implement checks or cleanup.

Caller evidence: Production alloc/src/vec/drain.rs keep_rest wraps self in ManuallyDrop, then moves unyielded elements/tail and updates vector length itself before suppressing Drop. ManuallyDrop owns none of that consistency work. coretests/tests/manually_drop.rs verifies suppressed and explicitly requested destruction. High reflects displaced lifecycle/safety duties, not unsafe syntax or documentation volume.

Provenance: Rust 1.98.1 Homebrew; rustc commit 48a229ceaefd4985c50990b14116b6d856af0985 dated 2026-09-01; aarch64-apple-darwin

Snapshot SHA-256: `c21100b507893a11ee27b0711be3ac3fe6cc970b96bba55a7d3bae5978209818`.

## rust-vec-drain

Frozen band: 0–25 (low).

Hidden duties: Owns removal iteration, tail relocation and length restoration on drop; DropGuard restores state when element destruction panics; handles zero-sized elements and keep_rest relocation without double drop.

Retained obligations: Caller chooses a drain range at construction, consumes/discards the iterator and optionally keeps remaining items; it does not implement tail repair or unwind cleanup. External range validation in Vec is not credited to this snapshot.

Caller evidence: alloctests/tests/vec.rs test_drain_range checks preserved head/tail without caller repair; test_drain_leak catches a panicking destructor and checks remaining vector/drop count. The low boundary owns substantive lifecycle/invariant duties. It is also a real caller witness for the distinct ManuallyDrop boundary.

Provenance: Rust 1.98.1 Homebrew; rustc commit 48a229ceaefd4985c50990b14116b6d856af0985 dated 2026-09-01; aarch64-apple-darwin

Snapshot SHA-256: `18d89167a973e539da58924d91bea87610812191d587b9e517c062ca62067ad3`.

## Scope and rejected alternatives

Explored local source listings in goname, cargo-coupling, career-ops, ap and icon packages and available standard libraries. cargo-coupling web/graph.rs was rejected as a high because it owns substantive graph translation/normalization; declaration-only carriers and icon exports were not suitable protocol highs. Go lock acquisition helpers were inspected but are private platform boundaries, so public stdlib CreateTemp/MkdirTemp provides a clearer low comparator. These decisions preceded any candidate scoring.

The Go high hides useful capability negotiation, acknowledged explicitly; actual ReverseProxy use nevertheless owns the protocol and cleanup. The TypeScript high hides token bookkeeping, acknowledged explicitly; the two production routes still implement lifetime and exclusion. Rust safety documentation is evidence of obligations, never hidden implementation credit.

Each target and witness was copied byte-for-byte, with origin and hash. All target origins and hashes were checked against four prior manifests. Witnesses are review evidence only, excluded from scoring inputs. Low/high rankings require at least 25 points and do not assert capability equivalence.

## Limits and freeze policy

- Reviewer previously read current SHALLOW calibration and connected-result flow implementation during independent code reviews. Independence means candidate scores unseen and separation from the implementing agent, not total scorer-source blindness.
- Reading the existing graded-independent-review rubric also exposed historical scores for older cases; none of these six target scores was seen. Prior manifests were used for origin/hash exclusion and schema, not evaluated.
- Purposive mechanism sample, not representative distribution or enterprise capacity evidence. High bands are falsifiable source judgments; useful primitive APIs can intentionally retain obligations, so high does not mean defective software.
- Whole-file snapshots are analyzed individually with dependencies absent. Standard-library internals and Rust unstable/const syntax may be unsupported; retain numeric estimates and honest limitations, not claims of compilation or full semantic context.
- Witnesses are not scored inputs. No synthetic source was authored. Large witnesses were read at the relevant named callers rather than every unrelated operation.
- Cross-case rankings compare responsibility depth, not capability-equivalent transformations. The low is not proposed as a replacement for its language high.
- After first evaluation preserve failures and labels. Implementation tuning using this sample consumes holdout status and requires a new fresh holdout.

Manifest SHA-256: `cd639be03a2f4089654b25600ab39ddbfa176afeb7ba284d1e18ace9c08fabf0`. No candidate scoring was invoked during selection.

## Additional source-review challenge: are these merely useful controls?

The frozen Go high is **not** based on callers selecting optional methods. The concrete exposed boundary crosses ownership states. `Hijack` returns both a connection and buffered reader/writer, but ResponseController stores no taken-over state, installs no cancellation handler, performs no framing/flush sequence and owns no connection close. In the production `ReverseProxy.handleUpgradeResponse` witness (lines 825–870), immediately after `rc.Hijack()` the caller starts a cancellation watcher for the backend, defers connection cleanup, converts the response into header-only form, writes headers to the returned buffer, flushes that buffer, launches copying in both directions and waits for completion. These obligations are coupled: response bytes must be framed and buffered headers flushed before the stream is used, and cancellation/lifetime must cover both ends. The controller does not combine these actions or enforce their ordering. The supplied deadline tests add a separate coupled state example: after an exceeded write deadline, setting a future deadline does not restore progress; callers still interpret that sticky state and choose recovery. Flush timing is likewise owned by the real maxLatencyWriter timer/stop sequence, not the controller.

There is genuine counterevidence: the Go facade eliminates repeated optional-interface assertions, recursive wrapper unwrapping and differences between Flush and FlushError. Those duties are explicitly credited in the judgment. They are dispatch conveniences rather than ownership of the larger resource transition and response sequence. The high band is a falsifiable engineering judgment about this narrow controller boundary, not an assertion that its underlying HTTP server does no work or that every API with optional controls is shallow. The selected production witness is what separates this case from a harmless broad namespace.

The TypeScript registry similarly is not high merely because acquire/release/query are separate. Its declared invariant is exclusion between a mutating worker and tracker deletion. The actual writer route must acquire **before** starting the worker and release at an appropriate terminal point on all close/cancel paths; the separate delete route must consult the registry and maintain its own delete-in-progress lifetime. The registry never receives the operation being protected and therefore cannot guarantee the pairing or relate a released token to process termination. Its token-set consistency is real hidden work, but the safety-critical operation lifetime and exclusion remain outside it.

The Rust high is a concrete behavioral type, not a declaration-only carrier: it implements new, into_inner, unsafe take/drop, dereference and value traits. `take` explicitly leaves the container's state unchanged after moving the value out; `drop` explicitly leaves a value-shaped memory region after destruction. The caller must prohibit subsequent safe access and second destruction, and coordinate drop order/unwinding itself. Transparent representation and destructor suppression make those obligations intentional; they do not enforce them. Safe `into_inner` is a genuinely safer subpath because it consumes its wrapper and is acknowledged as such. `Drain.keep_rest` demonstrates the other path: it suppresses ordinary cleanup and must repair vector storage/length itself. Compiler attributes, const/unstable syntax and missing standard-library crate context may limit analyzer recognition, but that is a reported estimation limitation, not evidence that this executable unsafe boundary is only a passive declaration.

This challenge review adds rationale only. The manifest, source snapshots, high/low labels, ranges and ranking thresholds remain unchanged and no candidate scores have been run or inspected by this reviewer.
