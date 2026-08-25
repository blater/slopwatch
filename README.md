# Slopwatch

Slop causes us humans painful extra cognitive load, while agents have DEEP slop tolerance.
But we do see them drop in performance as slop causes reasoning chains to lengthen,
evidence to conflict, distraction to increase. Planning becomes more elaborate with more subtasks,
they start overlooking constraints more often, forgetting goals, and prioritising irrelvancies.

*Slopmark* is your quality measuring tool.
It finds design and abstraction smells in Go, Java, TypeScript, and Rust, giving the code a weighted score
based on coupling, cohesion, module depth, and cognitive complexity. It is not a substitute for a linter.
It is designed to be run by agents and lets you give them *one clear and simple KPI* to stop the descent to slop.
Give them a simple rule: keep the score under target, pass the gates, and rework anything over it until it is clean.

*Slopwatch* is your window into code health.
Use it to keep an eye on what is creeping upwards and how refactors are progressing.

## Install and usage

The Homebrew package includes the `slopmark` analyzer and `slopwatch` live dashboard:

```sh
brew tap blater/tap
brew install slopwatch
```

The TypeScript analyzer requires Node.js 22 or newer. Homebrew installs that
runtime dependency automatically.

For a source checkout:

```sh
git clone https://github.com/blater/slopwatch.git
cd slopwatch
make build
```

Analyze one or more directories or files. Supported source files are `.go`,
`.java`, `.ts`, `.tsx`, `.mts`, `.cts`, and `.rs`:

```sh
slopmark src
slopmark myproj/src anotherProj/prod/src
```

Open the live dashboard with `slopwatch` (equivalent to `slopmark --follow`):

```sh
slopwatch --limit 200 .
```

In the dashboard, use the arrow keys or `j`/`k` to move, `g`/`G` to jump to
the first or last result, `v` to view the selected file, `f` or `/` to find,
`h` for topic-based help, and `q` to quit. Choose Settings → Appearance to
switch between the dark and light themes.

Dashboard appearance, columns, sorting, scoring weights, and interaction
defaults persist in a versioned, user-editable TOML file. See
[Preferences](docs/preferences.md) for its location, complete schema, and
command-line precedence rules.

### Agent-assisted fixes

Set `OPENAI_API_KEY` in the environment that launches Slopwatch, highlight a
file, and press `x`. The Fix form lets you choose the score target, one or more
focus metrics, change scope, agent profile, model, effort, delegation mode,
validation plan, delivery workflow and branch name. Advanced mode replaces the
editable task body while retaining Slopwatch's locked scoring and write-scope
envelope.

The built-in GPT profile uses the OpenAI Responses API as a controlled coding
agent. It has no shell, process, Git or ambient filesystem access: candidate
reads and writes pass through Slopwatch-controlled tools. A Codex CLI profile
is also included, but it remains visibly **NOT RUNNABLE** on platforms where
its exact filesystem and process-containment probes do not pass.

Fix jobs use detached Git worktrees, so multiple non-overlapping jobs can run
at once and new jobs can be submitted while others are active. Press `Tab` to
switch between Files and Agents, or `A` to jump to Agents. Expand a job to see
its targets and compact metric state; `C` cancels only the selected eligible
job. Agent completion is not treated as success: Slopwatch freshly analyzes
the candidate and keeps its diff for review.

Settings contains Agents, Fix defaults, Concurrency & retention, Validation,
and Git & pull requests. Agent profiles store only authentication references
such as `env:OPENAI_API_KEY`, never the credential itself. Pull-request
publication can optionally require a ready validation plan. Operational
constraints are visible settings: concurrency, retention and actor limits are
global; provider turn/tool/token/file/context budgets belong to the selected
agent profile; validation time/output limits belong to each trusted check, and
candidate workspace file/directory/byte ceilings are visible in Validation.
Model turns, tool calls and token checks default to no Slopwatch-imposed cap,
active attempts have no wall-clock timeout, and Retry is an explicit unlimited
job action. The hardened Docker
validation executor is enabled only when
`SLOPWATCH_FIX_CONTAINER_IMAGE` names an immutable image digest and
`SLOPWATCH_FIX_DOCKER_HOST` names an explicit local `unix://` daemon socket;
`SLOPWATCH_FIX_DOCKER_EXECUTABLE` must name the canonical absolute path of a
non-writable Docker CLI outside the repository, and
`SLOPWATCH_FIX_EXECUTABLE_MAP` must be a JSON object mapping each trusted host
validation executable to its exact absolute path in the image (for example
`{"/usr/bin/go":"/usr/local/go/bin/go"}`). These environment variables are a
temporary typed installation bridge until the properties-file PR lands;
adapters and services never read them directly. If any property is absent or
invalid, validation is safely unavailable without preventing candidate-only
fixes. Build the installation image with
`make build-fix-validation-image FIX_VALIDATION_BASE=name@sha256:...`; its
immutable base must already contain the absolute executables named by the
trusted validation plans. The target performs no package installation or
network access and prints the resulting image ID. See the detailed
[agent-assisted fix plan](docs/agent-fix-plan.md) for the image and confinement
contract.

Pull-request delivery (draft or ready for review, as configured) additionally requires
`SLOPWATCH_FIX_GH_EXECUTABLE` to name the canonical absolute path of a
non-writable GitHub CLI outside the repository. Slopwatch checks `gh`
authorization and the exact `github.com/owner/repository` target before job
admission and again before publication; it does not select either CLI from the
ambient `PATH`.

![Slopmark follow-mode dashboard](docs/follow-mode.svg)

Common commands:

| Command | Purpose | Example |
| --- | --- | --- |
| `slopmark [TARGET ...]` | Analyze directories or files | `slopmark src` |
| `slopwatch [TARGET ...]` | Follow the live ranking dashboard | `slopwatch .` |

Common analysis options:

| Option | Purpose | Example |
| --- | --- | --- |
| `-c`, `--compact` | Show only score and path in text output | `slopmark --compact .` |
| `-f`, `--follow` | Open the live, scrollable ranking dashboard | `slopmark --follow --limit 100 .` |
| `--trend-window DURATION` | Set the follow-mode movement and edit-highlight window | `slopwatch --trend-window 30m .` |
| `--include-tests` | Include test source files | `slopmark --include-tests .` |
| `--follow-symlinks` | Follow symlinks found inside target directories; an explicitly named symlink target is always followed | `slopwatch --follow-symlinks src` |
| `--typescript-types` | Enable slower compiler-aware TypeScript type-safety analysis | `slopmark --typescript-types .` |
| `--limit NUMBER` | Return at most this many ranked files | `slopmark --limit 20 .` |
| `--pass-score SCORE` | Pass files scoring at or below this value | `slopmark --pass-score 100 .` |
| `--format json` | Emit the standard JSON report | `slopmark . --format json` |
| `--use-cache` | Reuse verified cached analysis units; without this, `slopmark` only updates the cache | `slopmark --use-cache .` |

`--pass-score` considers every analyzed file. Analysis returns 0 when all
files pass, 3 when any file does not pass, and 2 for analysis errors.

## Report measurements

Lower numbers are better. A routine is a function, method, or constructor.
`-` means that the analyzer does not supply that measurement. These short
descriptions are also available in the dashboard with `h`.

| Column | Meaning |
| --- | --- |
| `SCORE` | Weighted sum of all enabled metrics and rules. Lower is better |
| `COG` | Cognitive effort needed to understand nested decisions. Lower is better |
| `NPATH` | Number of possible execution paths. Lower is better |
| `CYCLO` | Cyclomatic complexity - independent control-flow paths. Lower is better |
| `SHALLOW` | Functionality delivered per unit of interface complexity. Higher is worse |
| `CPL` | Maximum number of foreign types referenced by a type. Lower is better |
| `GOD` | Responsibility concentration in a type. Keep this low |
| `PATH` | Source file being measured |

### SCORE

`SCORE` adds the weighted contributions from the enabled components supported
for the file's language. It does not add the raw values shown in the report.

Type-safety checks are disabled by default because constructing and validating
a repository-wide TypeScript compiler graph can dominate startup on mature
projects. They apply only to TypeScript. Enabling `TYPE SAFETY` in Settings →
Columns automatically runs compiler-aware analysis and refreshes the dashboard;
no restart or extra flag is required. For non-interactive reports, or to preload
the graph before opening the dashboard, use `--typescript-types`.

Deep-nesting checks are also disabled in the dashboard score by default. To
include them, enable `NESTING` in Settings → Columns. This keeps the separate
nesting penalty from silently adding to the nesting already reflected in COG.

```text
raw measurements
→ component aggregation
→ threshold and formula
→ weight
→ component contribution
→ SCORE
```

Below a component's threshold, its contribution is zero when its formula uses
a threshold. At or above the threshold, that formula sets the contribution.
For a component configured with `log-ratio`:

```text
contribution = weight × (1 + log₂(value / threshold))
```

The score sums component contributions through their configured axes. A
missing or unavailable component contributes zero and marks the file
incomplete; it does not silently become a raw zero measurement.

### COG — cognitive complexity

COG follows the Sonar/PMD cognitive-complexity model. It estimates the mental
effort needed to understand a routine. Starting at zero, the calculation adds:

```text
if/loop/switch          1 + current nesting depth
else                    1
new boolean sequence    1
conditional expression  1 + current nesting depth
labeled jump            1
recursion               1
```

Nesting increases while a structural decision's body is visited. The report
shows the maximum routine value and the number of routines measured.

### NPATH — NPath complexity

NPATH counts distinct acyclic execution routes through a routine. Statement
sequences compose their routes; branches add their outcomes; boolean and
conditional expressions contribute their short-circuit outcomes; loops retain
the zero-iteration route; and a switch sums its case routes plus the no-match
route when there is no default. The implementation uses arbitrary-precision
integers so large results do not overflow.

### CYCLO — cyclomatic complexity

CYCLO is PMD-style cyclomatic complexity:

```text
CYCLO = 1 + decision increments
```

An `if` or loop adds one, each non-default switch case adds one, boolean
`&&`/`||` and conditional expressions add one for each decision, and explicit
jumps and panics are accounted for where the language adapter exposes them.
The report shows the total across routines and the maximum routine value.

### SHALLOW — module shallowness penalty

SHALLOW is an Ousterhout-inspired penalty: a module is deeper when it provides
more useful functionality through a smaller caller-visible interface. Higher
SHALLOW is worse. The calculation uses a COSMIC-inspired static approximation
of functional capability, not LOC or private implementation size.

*Formula*
```text
F = Functionality
I = InterfaceCost
D = DepthRatio

F = entries + exits + reads + writes
I = public operation count
    + recursive costs of their parameter and result type shapes
    + public type/state surface costs
D = F / I

depth penalty = 100 × max(0, 1 - D / D_ref)
leakage penalty = 100 × min(1, exposed representation / I)
SHALLOW = round(clamp(0, 100,
    0.80 × depth penalty + 0.20 × leakage penalty))
```

An entry is data or an event supplied to a public operation. An exit is a
result or observable output. Reads and writes are recognized persistent-store
movements. The analyzer estimates these terms from public operations, their
signatures, public types, and exposed mutable representation.

`D_ref` is selected from caller-visible role evidence by the versioned
`role-shape-v2` policy. The report includes the raw `D`, the reference, and
the penalty components. A module with no identifiable public interface is
unavailable rather than being assigned a misleading zero.

For nested types, each wrapper and child contributes one. The structural
analyzers cap each type shape at 32 so one enormous type cannot dominate the
metric.

SHALLOW uses a threshold of `20`, a weight of `5`, and `log-ratio`. Therefore:

```text
SHALLOW < 20  → contribution 0
SHALLOW = 20  → contribution 5
SHALLOW = 40  → contribution 10
SHALLOW = 80  → contribution 15
```

### CPL — type coupling

CPL shows the maximum number of distinct foreign types referenced by any type
in the file. The displayed value is the raw coupling measurement; scoring uses
the separately configured contribution. With the default threshold of `20`,
values below `20` remain visible in CPL even though they contribute zero to
SCORE.


### GOD — responsibility concentration

GOD uses PMD's God Class signal, generalized to the type systems supported by
each analyzer. A type is a candidate when all three conditions hold:

```text
WMC >= 47       weighted routine complexity is high
ATFD > 5        access to foreign data is high
TCC < 1/3       type cohesion is low
```

WMC is the sum of routine complexities, ATFD counts distinct foreign data
accesses, and TCC is the proportion of routine pairs sharing access to state.
The analyzer applies the test to the type-level declaration for which it has
the required evidence. This may be a class, struct, record, or interface,
depending on the language and analyzer. The displayed GOD value is the
weighted penalty for the candidate; zero means the combined conditions did not
trigger, and `-` means unavailable.

### PATH

PATH is the source path only; it is not a quality measurement.

## Release

Configure GitHub Actions once. The setup script checks the source repository,
enables Actions, verifies that the Homebrew token can update the tap, and stores
the token as an Actions secret without writing it locally:

```sh
./actions-setup.sh
```

Then release with:

```sh
./release.sh X.Y.Z
```

Run the script locally from a clean working tree, using a version higher than
the latest local release tag. It pushes the current branch, creates and pushes
the release tag, then monitors the tag-triggered GitHub Release workflow. That
workflow builds and smoke-tests the packaged Go, Java, Rust, and TypeScript
analyzers before publishing them. Finally, the local script verifies the GitHub
assets and Homebrew formula.
