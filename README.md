# Slopslap

Slopslap finds design smells in Java and TypeScript (Rust is experimental),
and gives them a score weighted on coupling, cohesion, depth, and cognitive complexity. 

Slop builds up, as extraneous cognitive load for humans, we've all seen impossible spaghetti code 
which is painful to read, review, and reason about.

We've also all seen that agents have deep slop tolerance.  However, they suffer in different ways,
as reasoning chains lengthen, evidence conflicts, and distraction increases. 
Symptoms start to show - planning becomes more elaborate with more subtasks, they start overlooking 
constraints more often, forgetting goals, and prioritising irrelvancies. 

This tool condenses a complex mix of measures into *one clear and simple number* for agents - stay 
under that, gates pass, go over it & they have to rework until it's clean.

I use this in AGENTS.md as a *continuous forcing function*, pushing agents towards good. 

It is aimed larger projects where you want a solid, sustainable architecture, where each module
needs crystal clear focus & clean well defined interfaces.  

## Install 

Slopslap works as a CLI command, as an MCP server, or as a github action and runs on Python 3.10 or later.

Install it with:
```sh
git clone <repository-url> slopslap
cd slopslap
python3 -m pip install .
```

### Language Requirements

Each language you're going to run static analysis on has dependencies:

| Java: |
| *JDK 11+* - (we normally run on JDK 25) |
| *Maven;*  |
| The analyzer uses PMD 7.26.0 and is built on first use. |
| |
| TypeScript |
| *Node.js* 22 or later |
| *npm* |
| |
| Rust |
| - *Rust 1.78+* or later  (we use 1.97.1) |
| - Cargo |

You'll be prompted to install the analysis driver for your language the first time you run slopslap for
that language, otherwise you can preemptively install them with the "install" command:
```sh
slopslap install java
slopslap install typescript
slopslap install rust
```

The help message in `slopslap -h` will show you which analysis drivers are installed.


## Usage

The simplest usage is: `slopslap <project path>`

This will scan down the tree analysing files with `.ts`, `.tsx`, `.mts`, `.cts`, `.java` and `.rs` extensions.

Full command list:

| Command | Purpose | Example |
| --- | --- | --- |
| `slopslap` | Show help and installed analyzer status | `slopslap` |
| `slopslap analyze [TARGET ...]` | Analyze directories or files | `slopslap src` |
| `slopslap action add` | Add the GitHub Actions workflow | `slopslap action add` |
| `slopslap action list` | Show the managed workflow | `slopslap action list` |
| `slopslap action remove` | Remove the managed workflow | `slopslap action remove` |
| `slopslap action set-pass-score SCORE` | Set its optional pass score | `slopslap action set-pass-score 100` |
| `slopslap install LANGUAGE` | Install an analyzer | `slopslap install typescript` |
| `slopslap config` | Show configurations, analyzers, and components | `slopslap config` |
| `slopslap config edit [FILE\|global]` | Create or edit a configuration | `slopslap config edit` |

The `analyze` word may be omitted. Common analysis options are:

| Option | Purpose | Example |
| --- | --- | --- |
| `--config FILE` | Use an exact configuration file | `slopslap . --config team.toml` |
| `--limit NUMBER` | Return at most this many ranked files | `slopslap --limit 20 .` |
| `--pass-score SCORE` | Pass files scoring at or below this value | `slopslap --pass-score 100 .` |
| `--format json` | Emit the standard JSON report | `slopslap . --format json` |

`--limit 0` returns every result. The limit only controls returned rows;
`--pass-score` always considers every analyzed file.

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
| `DEEP` | Java and TypeScript: number of `if` chains reaching three levels. Rust: greatest control-flow nesting depth. |

Currently for Java we're able to take every measurement. 
TypeScript we can measure everything except the cyclomatic type total. 
Rust support is still weak. It gives `DEEP` as a best-effort measure; its other structural measurements show `-`.

## Configuration

Without `--config`, Slopslap selects the first existing configuration from:

1. `./.slopslap.toml`
2. `$XDG_CONFIG_HOME/slopslap/config.toml` or `~/.config/slopslap/config.toml`
3. `~/.slopslap.toml`

`slopslap config edit` opens the config file or editing (and creates it if it doesn't already exist)

## MCP server

The STDIO MCP server exposes three read-only tools:

| Task | Tool |
| --- | --- |
| Rank supported source files | `rank_files(directories=[], languages=["auto"], limit=10)` |
| Score exact supported files | `score_files(files=[...])` |
| Inspect configuration and analyzers | `get_config()` |

The analysis tools return the same report format as `slopslap --format json`.
All three tools are read-only.

Register the server with Codex:

```sh
codex mcp add slopslap -- slopslap-mcp
```

Or register it with Claude Code:

```sh
claude mcp add --transport stdio slopslap -- slopslap-mcp
```
