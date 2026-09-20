---
status: complete
baseline_commit: 801da93a46c708f9396f46d83df6d772c2ce41e3
workspace_snapshot: eb7fbca20e97d76372b2a9805de4b7e5778ba053
---

# Incremental scan delivery

## Scope

Update the existing master document, table and graph as files finish analysis.
Apply available updates on the existing approximate two-second UI tick; resort
and preserve selection. Keep scanning visible until the authoritative result arrives.

Java, Go, Rust and TypeScript must behave alike, alone or together. An initial
syntax preview followed by silence until completion does not satisfy this work.

## Implementation

- Retain shared compiler, package and flow context. Deliver completed file results
  from the existing computation loops; do not rerun analysis per file.
- Stream ordinary measurement and coverage records through the existing adapters
  and host. No separate file-progress record is required. Internal helper framing
  changes are limited to removing whole-response buffering.
- Prepare bounded source attribution once and join it with each file's semantic
  results. Reuse that preparation for final reporting.
- Replace each delivered component's observations, including an empty result.
  Preserve other components and remove obsolete boundary references.
- Scan delivery must not set edit backgrounds, new-file markers or movement
  arrows. Actual edits compare against the pre-scan values.
- Keep calculation readiness separate from semantic evidence quality. Finished
  estimated ratings are usable; unfinished calculations remain pending.
- Exclude pending totals from the completed-score graph. Publish final scores and
  evidence under the existing scoring and cache rules.
- Use existing scan generations to reject stale callbacks. Preserve cancellation,
  failure handling and final authoritative replacement.

## Completion checks

- Review component replacement, readiness, ownership and final-result parity.
- Keep focused regression checks in the existing suites; add no test framework.
- Run `make build`, including its packaged-distribution checks.
- Confirm the actual application receives repeated completed-file updates.
  Use Kafka for the Java application check and existing real projects for the
  other language paths. Do not restart broad performance benchmarking.
- Keep changed source files at or below slopmark 100 without changing scoring
  rules, weights or exclusions.
- Integrate without overwriting concurrent workspace edits and identify the
  runnable executable.

## Excluded

No new scheduler, timer subsystem, generalized progress framework, scoring
changes, unrelated refactoring or enterprise performance programme.

## Ownership

Luna/max agents complete host attribution and TypeScript changes. The parent
reviews the integrated change, fixes structural/UI integration, runs the shared
build and verifies the deliverable.

## Verified delivery

- `make build` passed, including packaged-distribution smoke checks.
- Ordinary records publish merged file results; unavailable evidence does not
  leave completed calculations pending.
- Scan updates remain visually neutral; actual source edits retain highlighting.
- The rebuilt UI scanned all 65 TypeScript files in this project without errors.
- Kafka's displayed completed count advanced through 44, 2,407 and 3,782 files;
  pending counts fell to zero. No analyzer failure appeared.
- Changed production sources remain at or below slopmark 100.
- Runnable executable: `build/slopmark`.
