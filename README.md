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

In the dashboard, use the arrow keys or `j`/`k` to move, `v` to view the
selected file, `f` or `/` to find, `h` for help, and `q` to quit.

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
| `--limit NUMBER` | Return at most this many ranked files | `slopmark --limit 20 .` |
| `--pass-score SCORE` | Pass files scoring at or below this value | `slopmark --pass-score 100 .` |
| `--format json` | Emit the standard JSON report | `slopmark . --format json` |

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
| `GOD` | God classes - bloated modules with a low-cohesion class score. Keep this low |
| `PATH` | Source file being measured |

### SCORE

`SCORE` is the sum of the contributions from the enabled measurements and
rules for the file:

```text
SCORE = Σ component contribution
```

Each contribution applies its PMD-style threshold and weight, so the displayed
score is a weighted penalty rather than an average of the visible raw values.

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

```text
F = entries + exits + reads + writes
I = operation cost + type cost + exposed state cost
D = F / I
depth penalty = 100 × max(0, 1 - D / D_ref)
leakage penalty = 100 × min(1, exposed representation / I)
SHALLOW = round(clamp(0, 100,
    0.80 × depth penalty + 0.20 × leakage penalty))
```

An entry is data or an event supplied to a public operation; an exit is a
result or observable output; reads and writes are recognized persistent-store
movements. The static implementation estimates these terms from public
operations, their parameters/results, public types, and exposed mutable
representation. `I` charges one per public operation, the structural cost of
each public signature type, and each exposed public state item. A module with
no identifiable public interface is unavailable rather than being given a
misleading zero.

The raw ratio `D` is retained for diagnosis; SHALLOW is the bounded penalty
shown in the report. See [`docs/depth-design.md`](docs/depth-design.md) for
the full definition and references. `D_ref` is selected from caller-visible
role evidence by the versioned `role-shape-v2` policy and is reported with the
measurement.

### GOD — God Class

GOD uses PMD's God Class signal. For each concrete class, the candidate test is:

```text
WMC >= 47       weighted methods per class is high
ATFD > 5        access to foreign data is high
TCC < 1/3       tight class cohesion is low
```

WMC is the sum of method complexities, ATFD counts distinct foreign data
accesses, and TCC is the proportion of method pairs sharing access to a field.
The displayed GOD value is the weighted penalty for the candidate; zero means
the PMD conjunction did not trigger, and `-` means unavailable.

### PATH

PATH is the source path only; it is not a quality measurement.
