# Repository instructions

## SHALLOW release priorities (user mandate, 2026-09-17)

Read [the P0 delivery directive](docs/shallow-p0-delivery-directive.md) before
changing SHALLOW, its reporting, acceptance tests or release criteria.

1. **P0 first: publish useful numeric ratings.** Applicable supported-language
   source must receive a SHALLOW rating in the normal report, ranking and SCORE.
   Incomplete semantic evidence must not turn its rating into `X`, blank or an
   omitted contribution. Report estimation and limitations separately.
2. **P0 second: enterprise performance.** Five minutes for 30,000 files is
   unacceptable. Measure and improve cold, warm and incremental performance;
   small-repository timings do not establish enterprise readiness.

These instructions supersede older documents that require estimates to be hidden
or defer enterprise capacity validation. Do not solve the first P0 by silently
calling every unknown responsibility zero, emitting blanket 0/100 scores, or
merely displaying the known misleading recognition-only 100 estimates. Deliver
a defensible bounded approximation across Java, TypeScript, Go and Rust.
Preserve honest evidence metadata and separate safety gates for automated fixes.

## No post-startup discovery (user mandate, 2026-09-22)

After startup, source changes must not trigger workspace-wide discovery or
planning scans. Maintain the file inventory, ownership and dependency indexes
from filesystem events. Existing-file edits must not enumerate directories or
reread unrelated sources for planning. New-directory enumeration is limited to
that new subtree; deletion uses known paths. Do not substitute whole-inventory
in-memory replanning for filesystem scans. No full-scan fallback, reconciliation
scan or background audit may bypass this requirement. Removing full rescoring
alone does not satisfy incremental refresh. Retain `.gitignore` monitoring for
now; its changes must not cause a full refresh or discovery scan.

## One test entry point (user mandate, 2026-09-19)

Local builds and CI must use the exact same command: `make build`, with no
workflow-specific flags or parameters. Plain `make` invokes it too. The Makefile
owns orchestration: build, package, and run the full test suite, including
packaged-distribution smoke tests. Keep shared test implementations in the repository
and invoke them through this common path. Preserve build caches; use
`make test-clean` explicitly when a clean run is needed.

Deployment (newer user mandate, 2026-09-22) publishes the existing successful
push/manual CI artifact for the exact release commit. It must not compile,
install toolchains, run functional tests or packaged binaries, or repackage the
archive. It may rename the archive and update the checksum filename while
preserving tested bytes. Missing, failed or expired CI artifacts stop deployment;
there is no deployment build fallback. Retain tag, credential, provenance,
checksum and Homebrew formula style checks. Publishing credentials and network
availability are external requirements, not additional test suites.
