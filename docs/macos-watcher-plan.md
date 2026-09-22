---
status: done
baseline_commit: 19a52868c0e121b01b4bb82dfd5dcf06ae1e9c12
context:
  - AGENTS.md
  - docs/shallow-p0-delivery-directive.md
---

# macOS watcher descriptor fix

Status: implemented and validated through `make build` on macOS, 2026-09-22.
This does not establish enterprise capacity.

Implementation scope instruction: implement only this agreed plan. Do not add
owners, new exit criteria, platform/version compatibility projects, test scope,
`.gitignore` edge-case handling or extended recovery. Mention any such finding
separately and continue scoped work. The plan is already approved; no additional
planning/approval checkpoint or replacement specification is required.

## Goal

Eliminate macOS startup failures caused by per-file watch descriptors, while
preserving event-driven updates and uninterrupted user sessions.
Provide an explicit `[r]escan` action that repeats startup verification in-session.

## Success criteria

- macOS uses FSEvents file notifications; adding source files does not add
  persistent file descriptors. Native subscriptions scale with distinct watch
  roots, not files or every inventoried directory.
- A short real-backend test starts successfully with more files than the test
  process's descriptor limit, receives changes, and releases watcher resources.
- Edits, atomic saves, creates, renames and deletions update the existing
  inventory and affected analysis. Existing-file edits enumerate no directories;
  new-directory enumeration stays within that subtree; deletion uses known paths.
- Notification loss never triggers a rescan, restart, automatic popup or session
  termination. Monitoring continues with the footer notice specified below.
- Only an explicit user request may repeat full startup discovery and checks.
  `[r]escan` refreshes inventory, indexes and results without restarting the app.
- Linux and Windows retain their current backends. `.gitignore` monitoring and
  existing source/target policies remain intact.
- Implementation validation uses plain `make build`, including shared tests and
  packaged-distribution smoke tests. Short checks establish this fix, not 30K
  enterprise readiness.

## Evidence and scope

`workspace/monitor_watch.go:addDirectory` registers each directory through
`fsnotifyBackend.Add` before classifying its entries. On macOS, fsnotify's kqueue
backend opens descriptors for the directory and individual children. Filtering
source files afterward cannot prevent this resource growth.

`TestDirectoryWorkDoesNotGrowWithUnrelatedInventory` uses a fake backend whose
`Add` records strings. Its 30,000-entry case cannot establish real watcher
capacity. Retain its inventory-contract purpose; do not cite it as OS capacity
evidence.

Scope is the macOS backend, startup event collection and resource cleanup,
notification-loss presentation, user-requested rescan, and focused checks. Screen-update cadence,
analyzer performance, Linux/Windows backend replacement, benchmark redesign and
general error-system redesign are excluded. The repository's P0 directive and
no-post-startup-discovery mandate continue to apply to automatic work. The user's
2026-09-22 instruction explicitly authorizes a full rescan on demand; it does not
authorize event-triggered, scheduled or error-triggered rescans.

## Implementation sequence

1. **Select the macOS integration.** Use a maintained FSEvents binding with
   file-level notifications, behind the workspace backend boundary. Subscribe
   once to each distinct required root, covering descendants natively. Inventory
   registration of a child must not recreate a stream, enumerate its siblings or
   add a native per-child watch. Preserve authorized external targets without
   expanding discovery scope.

   Selected binding: `github.com/fsnotify/fsevents v0.2.0`. Enable cgo for the
   Darwin application build/tests in the common Makefile path. Other platform
   builds retain their existing configuration. No separate build command,
   custom native watcher engine or kqueue fallback.

2. **Collect during startup.** Establish native delivery before startup inventory
   traversal. Use the library's standard synchronous callback/channel handoff
   with a dedicated receiver; keep analysis, rendering and filesystem traversal
   off that handoff. Retain startup changes until inventory initialization completes, then
   apply them through the existing event path. Keep inventory mutation serialized
   rather than racing startup traversal against the current event handler.

3. **Adapt file events.** Translate native notifications into the existing path
   and dependency updates. Resolve a notified path's current state and known
   inventory membership; do not assume FSEvents rename flags identify which end
   of a rename occurred. Preserve atomic-save semantics and existing symlink,
   ignore and explicit-target policies. No source event, `.gitignore` event or
   ambiguous directory notification authorizes rediscovering an existing subtree.
   An unresolvable loss-of-detail notification uses the warning path below.

4. **Coalesce intake work.** A recursive root stream also receives events from
   ignored subtrees that the current watcher never registers. Before
   queue admission, discard known exclusions using existing in-memory policy
   state; preserve explicit targets and relevant `.gitignore` notifications.
   This filter must not read files, enumerate directories or rebuild policies
   per event. Do not discard unknown new paths merely because they are absent
   from the inventory.

   Coalesce pending events by path, preserving combined
   operation flags and final-state handling. Retain one pending entry per path
   without a count limit; remove entries as they are delivered. Do not discard
   changes because of backlog size. This supersedes the earlier bounded-buffer
   proposal by explicit user instruction. Do not create an event log, goroutine
   per event, or full-inventory sweep. If native delivery loses information,
   record one notification-loss warning and continue accepting subsequent events.
   Ordinary repeated changes to one path are not errors.

5. **Close resources.** Stop streams and release callbacks/handles on normal
   shutdown and failed initialization, including partial registration. No retry
   loop, automatic restart, alternate watcher or reconciliation fallback.

6. **Present notification loss without interruption.** Implement the footer and
   user-opened explanation described below. Keep unrelated error handling intact.

7. **Expose manual rescan.** Reuse the startup inventory and analysis/check path
   through an explicit in-session command, as specified below. Do not implement
   a separate scanner or invoke application startup/shutdown itself.

## User-requested rescan

- Add `[r]escan` to the normal RHS footer on Files and Agents views and to help.
  It remains available even when a warning temporarily replaces footer hints.
  Bind it at the main-view level; preserve existing `r` actions in forms, job
  details and settings, and normal text entry.
- Repeat the same workspace discovery, ignore-rule loading, inventory/index
  initialization and cache-validating analysis performed at application startup,
  using current targets and settings. Include previously missed creations and
  deletions. Reuse valid caches as startup does; this is not a cache purge or
  unconditional reanalysis of every file. Merely rescoring the old inventory
  would not satisfy the action.
- Keep the session, current view and existing results usable during the work.
  Preserve selection where its file still exists. Keep native event collection
  active, then apply changes received during the scan to the refreshed inventory
  using the same startup event handoff. Do not rebuild the UI or replay its logo.
- Serialize with existing analysis: keep at most one pending user-requested
  rescan, with repeated `r` presses coalesced. Never run competing full scans or
  automatically repeat a completed or failed rescan.
- On successful completion, clear the earlier notification-loss warning only
  if no further loss occurred during the rescan. A failed rescan must not imply
  that results are current. This adds no automatic recovery or retry path.

## Footer and explanation

- Location: right-hand side of the existing bottom keyboard-shortcut footer, on
  both Files and Agents views. Suggested text: `! Some file changes may be missed`.
  Shorten to `! Changes may be missed`, then `! warning` at narrow widths. Retain
  a visible `!`. Temporarily replace standard RHS functions when space is short;
  do not add a row or alter table selection, scrolling or focus.
- Dark theme: dark-red background and yellow text. Light theme: light-red
  background and black text. Use theme-aware styling, including when the theme
  changes while the notice is visible.
- Remove the notice 60 seconds after the latest occurrence and restore the
  normal footer. Repeated occurrences update one warning's count and timestamp
  and extend its deadline; they do not queue notices or create timers per event.
  Use the existing UI tick/deadline mechanism, with no new polling loop.
- Retain that one session-local explanation after the footer expires. Pressing
  `!` from the main Files or Agents view opens it; do not intercept `!` in text
  entry or another modal. With no warning recorded, `!` does nothing. Include
  the shortcut in existing help.
- Reuse the existing error popup's layout, wrapping, scrolling and close controls
  with a warning title. Store this warning separately from ordinary errors.
  Do not call `showRuntimeError` on arrival: it automatically opens an overlay.
  Closing details returns to the previous view without deleting the retained
  warning. Further occurrences must not reopen or refocus the popup.
- Explanation: “Filesystem changes arrived faster than notifications could be
  processed, or the filesystem reported incomplete change details. Some displayed
  results may be outdated. Monitoring is continuing. Close this message and press
  r to repeat the startup checks and refresh the workspace.” Show occurrence
  count and latest time. Do not invent an exact number of missed files or
  recommend restarting. The recommendation is optional; opening or dismissing
  the explanation never starts a rescan.

## Focused validation

Add only the checks needed for the agreed behavior, through the common test path:

- Real macOS backend: a small temporary tree in a subprocess with a controlled
  descriptor limit; create more files than the limit before starting the watcher.
  Assert successful startup, bounded descriptor growth as file count rises,
  delivery of ordinary file operations, and resource release. Use sufficient
  descriptor headroom for the test runtime. No analyzer or long capacity run.
- Retain real-backend ordinary-operation coverage on Linux/Windows where those
  hosts are available; do not claim a platform passed without running it.
- Existing inventory instrumentation: no directory enumeration for existing-file
  edits, only new-subtree enumeration, and known-path deletion. Verify startup
  changes survive initialization and `.gitignore` changes do not trigger discovery.
- Inject a loss notice to check continued event processing, bounded retained
  warning state, footer placement/colors, one-minute expiry, repeat coalescing,
  and explicit `!` opening. Use controlled time, not a one-minute sleep.
- On a small fixture, verify explicit `r` rediscovers missed additions/deletions
  through the startup check path, retains changes arriving during the scan, and
  reuses valid caches. Verify repeated presses cannot create concurrent scans,
  and warning arrival/expiry/dismissal cannot start a rescan. Update tests that
  assert startup is the only full scan to recognize this explicit user action.
- Record short before/after startup time, descriptor count and retained memory
  on the same fixture. Investigate regressions rather than accepting a descriptor
  fix that introduces repeated traversal or materially higher memory use.

Do not run the full build or long tests merely to review this document.

## Performance and scope review

The design removes the identified cause: FSEvents subscriptions replace kqueue's
per-file descriptors. This remains a design conclusion until the real-backend
check passes. It does not establish overall scan speed or enterprise readiness.

Constraints preventing replacement bottlenecks: root-level subscriptions;
callbacks whose work is independent of inventory size; exclusion filtering before
queue admission; path-coalesced pending work with no path-count limit;
serialized inventory mutation; no automatic post-startup discovery or
full-inventory replanning; one warning record and one deadline; reuse of existing
UI ticks. Manual rescan has startup-equivalent cost only when explicitly
requested, reuses its implementation/caches, and adds no idle or per-edit work.

The previously proposed restart/recovery flow is removed. Event-history replay,
automatic reconciliation, background audits, watch-limit tuning, new settings and
general notification infrastructure are excluded. No additional edge-case
handling is recommended in this plan. If implementation reveals a need for any
such expansion, describe it and ask the user explicitly before including it.

The binding/build decision is resolved above. Validation remains the common
`make build` command.

Review outcome: the performance review identified ignored-subtree events flooding
the new root stream; step 4 now requires inexpensive filtering before admission.
The edge-case review recommended no additional behavior beyond the agreed scope.
The subsequent user-requested rescan is an explicit scope addition: it reuses
startup checks without relaxing the ban on automatic rescans or restarts.
The structure review found the goal and criteria placed clearly before design and
validation details. No runtime tests were run for this document review.
