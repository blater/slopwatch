# Terminal startup delay: implementation and verification plan

Date: 2026-09-22

Status: planned; production changes and regression tests are not implemented.

## Objective

Prevent Bubble Tea's package initializer from querying the terminal background
before the backend enters `main()`. Keep the existing dependency versions and
explicit application theme selection. Do not fork, vendor-patch, or modify the
downloaded dependency cache.

This is a Bubble Tea v1 compatibility shim. It does not establish enterprise
analysis performance or alter SHALLOW ratings, reporting, or acceptance
criteria. Preserve unrelated worktree changes.

## Plan at a glance

| Step | Files or evidence | Result |
| --- | --- | --- |
| 1. Establish controls | `util/test-terminal-startup.py`; identified pre-fix executable | Permanent harness self-tests and a recorded real-defect baseline |
| 2. Set the early default | `go/internal/terminalinit/init.go`; `go/cmd/slopslap-go/main.go` | Explicit background value before Bubble Tea initializes |
| 3. Protect theme behavior | Existing theme tests under `go/internal/follow` | Saved light theme and later transitions render correctly |
| 4. Integrate process tests | `util/test-distribution.sh` | Packaged help and dashboard paths tested by the common build |
| 5. Validate | `make build`; direct-backend initialization trace | Query absence, bounded startup, clean shutdown, and full-suite result |
| 6. Resolve incident attribution | Affected-terminal trace; known-fast comparison if available | Separate verdict on the user's reported regression |

## Confirmed mechanism and limits of the diagnosis

Confirmed: terminal replies control the reproduced package-initialization stall.
Not established: whether this explains the whole reported 2–3 second delay, or
what made the slowdown newly visible. Completion of the mechanism fix must not
silently close either attribution question.

The current dependencies are Bubble Tea v1.3.10, Lip Gloss v1.1.0, and Termenv
v0.16.0. Bubble Tea's `tea_init.go` calls `lipgloss.HasDarkBackground()` during
package initialization. Without an explicit background setting, the call reaches
Termenv's terminal detection.

On the applicable Unix terminal path, Termenv sends both an OSC 11 background
query (`ESC ] 11 ; ? ESC \\`) and a cursor-position query (`ESC [ 6 n`). Termenv may
wait for both responses. The five-second timeout applies to individual byte-read
waits, not a strict upper bound on the entire exchange.

A controlled diagnostic used the existing `build/slopmark --help` executable in
a foreground controlling PTY with `TERM=xterm-256color` and
`GODEBUG=inittrace=1`. No source instrumentation or rebuild was required.

| Replies supplied by the PTY harness | Total runtime | Bubble Tea initialization |
| --- | ---: | ---: |
| None | 5.016 s | 5.005 s |
| Immediate background and cursor replies | 0.018 s | 0.25 ms |
| Immediate cursor reply only | 0.011 s | 0.14 ms |
| Immediate background reply only | 5.015 s | 5.005 s |
| Both replies delayed two seconds | 2.017 s | 2.001 s |

These are individual diagnostic observations, not a statistical benchmark. They
establish that the terminal exchange can cause the startup stall and that a slow
OSC 11 response alone is not the only explanation. The diagnostic harness and
raw trace were not saved as repository artifacts; the regression helper below
must make the relevant experiment reproducible.

A trace from the affected launch is required to attribute the reported 2–3
second delay to this mechanism. To investigate why the slowdown appeared
recently, compare a verified previously fast revision with current code under
the same terminal/PTY, environment, toolchain, and launch path. Record any
unavoidable differences. If that revision or affected terminal is unavailable,
report the missing evidence and leave attribution open; this historical
investigation is not a permanent build prerequisite.

The dependency version entered repository history in commit `05fdedf` on
2026-08-21, which does not prove what changed in the terminal. The earlier claim
of 1.6 ms through `tea.NewProgram()` was not independently verified in this review.

The `slopwatch` launcher has already entered its own `main()` when it starts
`slopmark`. The confirmed initializer wait occurs before the backend's `main()`.

## Production changes

### 1. Add an independent initialization package

Create `go/internal/terminalinit/init.go`:

```go
// Package terminalinit provides a Bubble Tea v1 compatibility shim that sets
// an explicit background before Bubble Tea's terminal detection initializes.
package terminalinit

import "github.com/charmbracelet/lipgloss"

func init() {
	lipgloss.SetHasDarkBackground(true)
}
```

Document the ordering invariant beside this code:

- The package imports only Lip Gloss; it must not depend directly or indirectly
  on Bubble Tea.
- Bubble Tea also imports Lip Gloss. Once Lip Gloss is initialized, terminalinit
  is eligible to initialize before Bubble Tea.
- Its full import path,
  `github.com/blater/slopwatch/internal/terminalinit`, sorts before
  `github.com/charmbracelet/bubbletea`.
- Go initializes the first eligible package in import-path order. Consequently,
  the explicit background value is established before Bubble Tea asks for it.
- Lip Gloss then returns the explicit value without invoking background
  detection.

This initialization order relies on a language-defined rule, but package/module
renames and new dependencies can invalidate the conditions. Keep the package minimal and
retain process-level regression coverage. Moving this call into `main()` or an
initializer in a package that depends on Bubble Tea would be too late.

### 2. Import it from the backend entry point

Add a side-effect import to `go/cmd/slopslap-go/main.go`:

```go
_ "github.com/blater/slopwatch/internal/terminalinit"
```

This source builds `slopmark`, which is also executed by the `slopwatch` launcher.
The change therefore covers both entry paths. Import-list position does not
control initialization order.

### 3. Preserve runtime theme configuration

Retain `ConfigureTerminalColours()` and `ConfigureTheme()` in
`go/internal/follow/table_view.go`. They explicitly enable TrueColor and set the
background mode for the selected theme before the UI runs.

The early `true` is the initial dark-background default, not a permanent theme
selection. Loading or switching to a light theme must still set the value to
`false`; switching back to dark must restore `true`.

The concrete consumer motivating the early value is Bubble Tea v1's initializer.
Do not turn this dependency workaround into a permanent application architecture
contract. Reassess the shim when replacing that dependency behavior.

Extend the saved-light-preference coverage in
`go/internal/follow/preferences_test.go` to check the initial rendered palette,
before any manual theme change. Verify both dark and saved-light starts in the
process tests, and extend existing theme coverage for dark → light → dark
rendering. Checking `lipgloss.HasDarkBackground()` is a supporting compatibility
assertion, not a substitute for checking visible theme behavior. Restore global
theme state on cleanup and do not run these global-state tests in parallel.

## Startup regression coverage

### Shared PTY helper

Add `util/test-terminal-startup.py`, using Python's standard library. Accept the
distribution's binary directory as an argument and run these commands:

```text
<unpacked distribution>/build/slopmark --help
<unpacked distribution>/build/slopwatch --help
<unpacked distribution>/build/slopmark --follow --config <temporary-config> <tiny-workspace>
<unpacked distribution>/build/slopwatch --config <temporary-config> <tiny-workspace>
```

Create a tiny local Go workspace and complete, valid preference fixtures in a
temporary directory. Use the application's existing configuration schema and
test conventions, with external agent/fix integrations disabled. Keep config and
user-data/cache paths isolated using the application's supported locations so
the test cannot read or modify the developer's normal state. Run the interactive
cases with a dark theme and with a persisted light theme.

For each invocation:

1. Create a controlling PTY and run the child in the foreground, with standard
   input, output, and error attached to the slave. A pipe or a PTY without the
   correct foreground relationship can skip the faulty code path and give a
   false pass.
2. Set a fixed terminal size, such as 120 columns by 40 rows, before executing the
   child. Set `TERM=xterm-256color` and `COLORTERM=truecolor`. Explicitly remove
   `CI` from the child environment: Termenv treats any nonempty value, including
   `CI=false`, as non-TTY. Remove inherited `TERM_PROGRAM`, `COLORFGBG`, `NO_COLOR`,
   `CLICOLOR`, and `CLICOLOR_FORCE` to make the terminal environment consistent.
   Do not modify the parent workflow's environment. Send no terminal replies or
   keystrokes until the interactive first-paint assertion succeeds.
3. Capture the complete byte stream across read boundaries, using monotonic
   timestamps for elapsed time.
4. For help, require a zero exit status and recognizable CLI help output. For
   interactive cases, recognize a rendered application frame, then send `q` and
   require clean shutdown with a zero exit status.
5. Reject an OSC 11 query or a cursor-position query. Match across read boundaries
   and handle OSC terminators appropriately.
6. Use a 15-second overall watchdog and a five-second shutdown deadline after
   sending `q`. Place termination of remaining test processes, reaping of owned
   child processes, and descriptor closure in an unconditional `finally` path,
   including read errors and assertion failures. Cover the launcher and backend
   process group; do not assume the parent can directly reap grandchildren.
7. Treat PTY-master `EIO` on slave closure as EOF on platforms with that behavior,
   then check the child's exit status. Do not treat unrelated read errors as EOF.
8. Report phase timings, exit status, and useful captured diagnostics on failure.
   Log only the controlled terminal variables and fixture locations, never the
   full inherited environment or secrets.

First paint must contain recognizable UI content, not merely escape sequences,
alternate-screen entry, or arbitrary output. Inspect the actual initial view
and choose stable visible application markers; if a startup overlay obscures
them, define explicit markers for that rendered view. Set terminal dimensions so
an empty or resize-only view cannot pass. Normalize ANSI output sufficiently to
recognize the frame across write boundaries, and separately check representative
rendered palette colors for dark and saved-light starts. This checks initial
display responsiveness, not completion of repository analysis.

Query absence remains the primary mechanism assertion. Also require help
completion within five seconds and interactive first paint within five seconds
of process launch, measured with a monotonic clock. These deliberately loose
secondary bounds prevent a query-free 10–14 second stall from passing. Keep the
bounds identical locally and in CI; do not use retries to conceal failures.
Record timings even on success. A query fails regardless of elapsed time.

Use `GODEBUG=inittrace=1` on a separate direct `slopmark` diagnostic invocation to
confirm that `terminalinit` precedes Bubble Tea. Keep this trace as implementation
evidence, not a permanent build assertion tied to Go's diagnostic text format.
Do not infer process ordering from the launcher's mixed trace stream; test
`slopwatch` through observable end-to-end behavior. Measure acceptance timings
without initialization tracing enabled.

### Permanent harness controls and real-defect baseline

On every shared test run, execute synthetic child modes through the same PTY
capture and validation path. They must demonstrate that the harness:

- Rejects OSC 11 and cursor-position queries individually, including split
  writes and both BEL and ST OSC termination. Use deterministic matcher input
  chunk tests as well: the OS can coalesce separate child writes.
- Rejects query-free output that exceeds the startup deadline. Use a short
  internal self-test deadline, not a routine ten-second sleep; production
  command limits remain fixed as specified above.
- Rejects nonzero exit, incomplete help, escape-only output, and missing frame
  markers; accepts an immediate valid synthetic output control.
- Runs the child with `CI` absent even when the harness's parent has `CI=true`,
  and with a foreground controlling terminal of the configured size.
- Cleans up on timeout and early validation failure. Expected rejection is a
  self-test success only when the specific intended failure reason is observed.

Synthetic controls protect the harness; they do not prove that the dependency
uses the same path. Before applying the production fix, also confirm that the
helper detects the emitted query from a real unmodified executable. Record its
SHA-256 checksum, `go version -m` metadata, source commit, working-tree status
and diff identity, terminal settings, elapsed time, and relevant trace output.
Do not claim an existing binary matches the current checkout without provenance.
If provenance is missing, build through `make build` before editing production
code and retain an isolated baseline copy outside the final archive. Do not
commit an old binary or make it a permanent test dependency.

For a baseline build with the new harness integrated, the specific expected
query failure must be reported as a negative control, not as a passing build.
After the fix, the same shared path must pass normally.

### Common build integration

Invoke the helper from `util/test-distribution.sh` against the executables
extracted from the archive that script already creates. Place this check beside
the existing packaged CLI help smoke checks; avoid duplicating the helper's
assertions in shell or release YAML.

The required execution path remains:

```text
make build → shared test suite → distribution packaging and smoke tests
```

Use exactly `make build` for implementation validation, with no workflow-specific
flags or parameters. Plain `make` continues to invoke the same path. Keep test
code in the repository and publish the tested archive without rebuilding it.
Use VERSION only to name the archive. Preserve build caches; a clean run is an
explicit `make test-clean` operation, not a prerequisite of this fix.

The Makefile already discovers Go source files under `go/internal`, so adding the
package should not require a compilation-rule change. Any helper dependency or
invocation adjustment belongs in the common build path. Verify Python 3 and PTY
availability on the supported macOS runner; missing prerequisites must fail
clearly, not silently skip the regression. No third-party Python packages are
needed. Report unrelated build failures accurately rather than claiming full
verification.

## Acceptance criteria

- Bubble Tea, Lip Gloss, and Termenv versions remain unchanged; `go.mod` and
  `go.sum` need no edits.
- Both packaged help entry points complete successfully in a silent controlling
  PTY within five seconds without emitting background or cursor-position queries.
- Both packaged dashboard launch paths render a recognizable first frame within
  five seconds, with dark and saved-light preferences, then quit successfully
  within five seconds. They emit neither forbidden query.
- Child environments explicitly exclude `CI`; the synthetic control verifies
  this even when the parent has `CI=true`.
- Direct-backend initialization tracing confirms the ordering during validation;
  permanent tests assert observable behavior without parsing `inittrace`.
- Saved light-theme initialization and later theme transitions render correctly.
- Permanent harness controls pass, including query matching across boundaries,
  latency rejection, and failure cleanup. The identified real pre-fix executable
  is rejected for the original defect during baseline validation.
- The full shared `make build` path passes, including the packaged checks.
- The handoff includes before-and-after evidence and two explicit verdicts:
  **mechanism fix** and **reported regression attribution**. If the affected
  terminal is unavailable, state: "The reproduced terminal-query stall was
  removed; the reported 2–3 second regression remains unattributed." If a
  known-fast comparison is unavailable, leave why it appeared recently open.

## References

- [Go program initialization rules](https://go.dev/ref/spec#Program_initialization)
- [Bubble Tea v1.3.10 initializer](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/tea_init.go)
- [Lip Gloss v1.1.0 renderer and explicit background setting](https://github.com/charmbracelet/lipgloss/blob/v1.1.0/renderer.go)
- [Termenv v0.16.0 Unix terminal exchange](https://github.com/muesli/termenv/blob/v0.16.0/termenv_unix.go)
- [Backend entry point](../go/cmd/slopslap-go/main.go)
- [Launcher entry point](../go/cmd/slopwatch/main.go)
- [Runtime theme configuration](../go/internal/follow/table_view.go)
- [Common build orchestration](../Makefile)
- [Shared packaged-distribution tests](../util/test-distribution.sh)
