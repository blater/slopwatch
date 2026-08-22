# Slopwatch

Slop causes us humans painful extra cognitive load, while agents have DEEP slop tolerance.
But we do see them drop in performance as slop causes reasoning chains to lengthen,
evidence to conflict, distraction to increase. Planning becomes more elaborate with more subtasks,
they start overlooking constraints more often, forgetting goals, and prioritising irrelvancies.

*Slopmark*  is your quality measuring tool
It finds design & abstraction smells in Go, Java, TypeScript, and Rust, giving the code a weighted score
based on coupling, cohesion, module depth, and cognitive complexity.  It is not a substitute for a linter.
It is designed to be run by agents and lets you give them *one clear and simple KPI* to stop the descent to slop.
Give them a simple rule - keep score under target, gates pass, go over it & rework until it's clean.
Use it as *continuous forcing function*, pushing agents towards good.

*Slopwatch* is your window into your code health.
Use this to keep any eye on what's creeping upwards, how refactors are really progressing, and where the slop is.



## Install

Slopmark is a native Go CLI and GitHub Action with all analysis targets bundled.

Install it with:
```sh
git clone <repository-url> slopmark
cd slopmark
make build
```

## Usage

The simplest usage is:
```sh
slopmark <project path>
```

This scans `.go`, `.java`, `.ts`, `.tsx`, `.mts`, `.cts` and `.rs` files.

Multiple files and directories may be combined into one sorted report:

```sh
slopmark myproj/src anotherProj/prod/src
```

For a live dashboard that remeasures only changed source units:

```sh
slopmark --follow .
```

![Slopmark follow-mode dashboard](docs/follow-mode.svg)

Full command list:

| Command | Purpose | Example |
| --- | --- | --- |
| `slopmark` | Show help and analyze the current directory | `slopmark` |
| `slopmark analyze [TARGET ...]` | Analyze directories or files | `slopmark src` |

The `analyze` word may be omitted. Common analysis options are:

| Option | Purpose | Example |
| --- | --- | --- |
| `--config FILE` | Use an exact configuration file | `slopmark . --config team.toml` |
| `-c`, `--compact` | Show only score and path in text output | `slopmark --compact .` |
| `-f`, `--follow` | Open the live, scrollable ranking dashboard | `slopmark --follow --limit 100 .` |
| `--trend-window DURATION` | Set follow-mode movement-indicator and edit-highlight window | `slopmark --follow --trend-window 30m .` |
| `--include-tests` | Include test source files | `slopmark --include-tests .` |
| `--backend LANGUAGE=BACKEND` | Override one language's analyzer | `slopmark --backend java=pmd .` |
| `--limit NUMBER` | Return at most this many ranked files (all by default) | `slopmark --limit 20 .` |
| `--pass-score SCORE` | Pass files scoring at or below this value | `slopmark --pass-score 100 .` |
| `--format json` | Emit the standard JSON report | `slopmark . --format json` |


`--pass-score` always considers every analyzed file.  When present, the text report starts
with a `PASS` column. Analysis returns 0 when all files pass, 3 when any file does not pass, and 2 for configuration or analysis errors.


## Report measurements

Lower numbers are better. A routine is a function, method, or constructor.
`-` means that the analyzer does not supply that measurement.

| Column | Meaning |
| --- | --- |
| `SCORE` | Weighted penalties from every enabled rule and metric, after waivers. Thresholds and weights come from the active profile, so compare scores only under the same profile. |
| `COG MAX/#` | Highest cognitive complexity of any routine / routines measured. Branches and loops add cost; nesting adds more because nested control flow is harder to follow. |
| `NPATH MAX/#` | Highest NPath complexity of any routine / routines measured. NPath estimates distinct routes through a routine without repeating a loop; branches can multiply the result. |
| `CYCLO TOT/MAX` | Largest sum across one type's routines / highest value for one routine. Cyclomatic complexity starts at one and rises at each decision point, such as a branch or loop. |
| `SHALLOW` | Ousterhout/Rising & Calliss/Bandi inspired module shallowness penalty from 0 (deep boundary) to 100 (shallow boundary). |
| `GOD` | Weighted God Class contribution to `SCORE`. `0` means it was evaluated but PMD's WMC/ATFD/TCC conjunction did not trigger; `-` means unavailable. |


## Configuration

Without `--config`, Slopmark selects the first existing configuration from:

1. `./.slopmark.toml`
2. `$XDG_CONFIG_HOME/slopmark/config.toml` or `~/.config/slopmark/config.toml`
3. `~/.slopmark.toml`

`slopmark config edit` opens the config file or editing (and creates it if it doesn't already exist)
