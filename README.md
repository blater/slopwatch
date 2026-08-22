# Slopwatch

Slop causes us humans painful extra cognitive load, while agents have DEEP slop tolerance.
But we do see them drop in performance as slop causes reasoning chains to lengthen,
evidence to conflict, distraction to increase. Planning becomes more elaborate with more subtasks,
they start overlooking constraints more often, forgetting goals, and prioritising irrelvancies.

*Slopslap*  is your quality measuring tool
It finds design & abstraction smells in Go, Java, TypeScript, and Rust, giving the code a weighted score
based on coupling, cohesion, module depth, and cognitive complexity.  It is not a substitute for a linter.
It is designed to be run by agents and lets you give them *one clear and simple KPI* to stop the descent to slop.
Give them a simple rule - keep score under target, gates pass, go over it & rework until it's clean.
Use it as *continuous forcing function*, pushing agents towards good.

*Slopwatch* is your window into your code health.
Use this to keep any eye on what's creeping upwards, how refactors are really progressing, and where the slop is.



## Install

Slopslap is a native Go CLI and GitHub Action with all analysis targets bundled.

Install it with:
```sh
git clone <repository-url> slopslap
cd slopslap
make build
```

## Usage

The simplest usage is:
```sh
slopslap <project path>
```

This scans `.go`, `.java`, `.ts`, `.tsx`, `.mts`, `.cts` and `.rs` files.

Multiple files and directories may be combined into one sorted report:

```sh
slopslap myproj/src anotherProj/prod/src
```

For a live dashboard that remeasures only changed source units:

```sh
slopmark --follow .
```

![Slopslap follow-mode dashboard](docs/follow-mode.svg)

Full command list:

| Command | Purpose | Example |
| --- | --- | --- |
| `slopmark` | Show help and analyze the current directory | `slopmark` |
| `slopslap analyze [TARGET ...]` | Analyze directories or files | `slopslap src` |

The `analyze` word may be omitted. Common analysis options are:

| Option | Purpose | Example |
| --- | --- | --- |
| `--config FILE` | Use an exact configuration file | `slopslap . --config team.toml` |
| `-c`, `--compact` | Show only score and path in text output | `slopmark --compact .` |
| `-f`, `--follow` | Open the live, scrollable ranking dashboard | `slopmark --follow --limit 100 .` |
| `--trend-window DURATION` | Set follow-mode movement-indicator and edit-highlight window | `slopmark --follow --trend-window 30m .` |
| `--include-tests` | Include test source files | `slopmark --include-tests .` |
| `--backend LANGUAGE=BACKEND` | Override one language's analyzer | `slopmark --backend java=pmd .` |
| `--limit NUMBER` | Return at most this many ranked files (all by default) | `slopmark --limit 20 .` |
| `--pass-score SCORE` | Pass files scoring at or below this value | `slopmark --pass-score 100 .` |
| `--format json` | Emit the standard JSON report | `slopslap . --format json` |

Every result is returned by default; `--limit 0` is the explicit equivalent.
The limit only controls returned rows;
`--pass-score` always considers every analyzed file.

Follow mode uses native filesystem notifications rather than directory polling.
On an edit it remeasures the exact Java or TypeScript file, the containing Go
package, or the containing Rust crate, then merges those rows back into the
complete in-memory ranking. The default 10-minute trend window controls the
movement indicator beside each score and the row highlight that fades from
bright to muted after an edit. A module receives an indicator only when its
own score changes: `↑`/`↓` represent movement of 1–4 places, while `⇈`/`⇊`
represent movement of 5 or more places. Modules passed by a changed module
remain neutral. A newly discovered file starts with a green dot; if it moves
into the top half or top tenth during its first 10 minutes, that dot becomes
amber or red respectively before normal movement indicators take over.

`SHALLOW` is a 0–100 shallow-module penalty: lower is better; green indicates a
deep boundary, amber a mixed boundary, and red a shallow or leaky boundary.
The approved definition measures useful functional capability per
caller-visible interface cost, with explicit information-hiding leakage. The
implementation migration plan and references are in
[`docs/depth-design.md`](docs/depth-design.md).

| Follow-mode key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Move through results or scroll file details |
| `Ctrl-F` / `Ctrl-B`, `Page Down` / `Page Up` | Move by a visible page |
| `Home` / `End` | Jump to the first or last result/detail line |
| `Enter` | Open the selected file's scrollable full analysis |
| `c` | Choose visible metric columns |
| `s` | Sort by score, COG, NPath, cyclomatic complexity, depth, God score, or filename; use `←` / `→` for direction |
| `h` | Show column meanings and close the help popup with `h`, `Esc`, or `q` |
| `r` | Request an explicit full rescan |
| `Esc` / `q` | Close a popup |
| `Ctrl-C` / `q` | Quit from the dashboard |

The overview intentionally omits rank: the active `▲` or `▼` marker identifies
the sort column and direction. The detail view is bounded to the visible
terminal, and every popup overlays the current results.

Test sources are excluded by default. Use `--include-tests` to analyze them too.

Java uses the bundled structural adapter by default. Use `--backend java=pmd`
for the pinned PMD implementation, for example when checking oracle parity or
when classpath-aware Java type resolution is important.

`--pass-score` is optional and inclusive. When present, the text report starts
with a `PASS` column. Analysis returns 0 when all files pass, 3 when any file
does not pass, and 2 for configuration or analysis errors.

## Report measurements

Lower numbers are better. A routine is a function, method, or constructor.
`-` means that the analyzer does not supply that measurement.

| Column | Meaning |
| --- | --- |
| `SCORE` | Weighted penalties from every enabled rule and metric, after waivers. Thresholds and weights come from the active profile, so compare scores only under the same profile. |
| `COG MAX/#` | Highest cognitive complexity of any routine / routines measured. Branches and loops add cost; nesting adds more because nested control flow is harder to follow. |
| `NPATH MAX/#` | Highest NPath complexity of any routine / routines measured. NPath estimates distinct routes through a routine without repeating a loop; branches can multiply the result. |
| `CYCLO TOT/MAX` | Largest sum across one type's routines / highest value for one routine. Cyclomatic complexity starts at one and rises at each decision point, such as a branch or loop. |
| `SHALLOW` | Ousterhout-inspired module shallowness penalty from 0 (deep boundary) to 100 (shallow boundary). |
| `GOD` | Weighted God Class contribution to `SCORE`. `0` means it was evaluated but PMD's WMC/ATFD/TCC conjunction did not trigger; `-` means unavailable. |

Go, Java, and TypeScript support every listed measurement. The built-in Java adapter uses
source syntax and PMD-normalized ranges; the optional PMD backend remains the
classpath-aware oracle. Go's CBO and God-Class inputs are
marked unavailable when required imported source is not part of the analysis.
TypeScript's type-level structural metrics use syntax-level references and
member-access facts; they are supported but are not classpath- or
project-resolution-backed.
Rust supplies the same source-level structural measurements through the bundled
`syn` adapter; macro-generated control flow is intentionally not expanded.

## Configuration

Without `--config`, Slopslap selects the first existing configuration from:

1. `./.slopslap.toml`
2. `$XDG_CONFIG_HOME/slopslap/config.toml` or `~/.config/slopslap/config.toml`
3. `~/.slopslap.toml`

`slopslap config edit` opens the config file or editing (and creates it if it doesn't already exist)
