# codehealth

`codehealth` gives coding agents a measurable forcing function for Java design
quality. It ranks production files by a weighted design-debt score, making a
complex set of [PMD](https://pmd.github.io/) signals easy to express as a simple
goal: lower the score, and work toward keeping each file below 100.

Scores combine cognitive, cyclomatic, and NPath complexity with nesting,
coupling, and God Class findings. The tool is stateless: it reports the current
project and does not maintain a policy ledger or catalog historical scores.

The tool automatically finds Maven and Gradle-style `src/main/java` directories
(including multi-module projects) and infers the Java language version from the
build files when possible.

## Example usage

Analyse the Java project in the current directory:

```sh
codehealth
```

Or pass a project directory explicitly:

```sh
codehealth /path/to/java-project
```

Pass several directories to analyze several package or module trees together.
Each directory is recursive. Project and module directories are narrowed to
their conventional production roots; source and package directories remain at
the scope supplied:

```sh
codehealth module-a/src/main/java/orders module-b/src/main/java/payments
```

Use repeatable `--file` arguments when the exact changed classes are known.
Directories and files can be combined, and module/build inference is performed
for each target:

```sh
codehealth --file src/main/java/example/Changed.java \
  --file src/main/java/example/AlsoChanged.java
```

Show a compact top ten and include an explanation of the scoring model:

```sh
codehealth /path/to/java-project --compact --limit 10 --explain
```

Fail the run if any production Java file scores above 100:

```sh
codehealth /path/to/java-project --max-score 100
```

The maximum is inclusive: a score of exactly 100 passes, while any unrounded
score above 100 fails. A failed quality gate exits with status 3 after printing
the ranked results and a summary of the failure to standard error. This is
useful for greenfield projects or as a final target; legacy projects need not
enable it immediately.

### Agent workflow

For feature work, run `codehealth` afterward and treat high-scoring changed
files as candidates for a follow-up simplification agent. Give that agent a
simple instruction:

> Lower the target file's codehealth score. Work toward a score below 100, and
> do not finish unless the score decreases without breaking tests.

When several agents cooperate on a feature, evaluate the completed result
rather than requiring every intermediate step to satisfy the final target.

### Structured output

Human-readable text remains the default. Request JSON when an agent or another
program needs stable structured results:

```sh
codehealth /path/to/java-project --format json --limit 0
```

The JSON report contains scores and raw contributing metrics for each returned
file, the exact weighted contribution made by each signal, a project summary,
and the result of an optional `--max-score` gate. The summary identifies the
target score, number of files above it, and current highest-priority file.

An agent runner may retain before-and-after reports when it wants mechanical
comparison without making that history part of `codehealth`. `--limit` controls
the returned file list in both formats; use `--limit 0` when the caller needs
every selected score. Gate evaluation always considers every analyzed Java file
regardless of the output limit.

For a non-standard project layout, specify one or more production source roots:

```sh
codehealth . --source-root app/java --source-root shared/java
```

Run `codehealth --help` for all options, including custom rulesets and an
explicit PMD Java language version.

## MCP server

The optional local MCP server exposes the same stateless analysis through two
read-only tools. Keeping these purposes explicit makes tool choice mechanical:

| Agent task | MCP tool |
| --- | --- |
| Find the next cleanup target | `rank_java_files(directories=[], limit=10)` |
| Check changed files or one known target | `score_java_files(files=[...])` |

`rank_java_files` accepts repository, module, source, or package directories
and recurses beneath them. With no directories it uses the current working
directory. `score_java_files` analyzes exactly the supplied Java files, returns
all of them in request order, and still infers the relevant module and Java
version for each file. Neither tool edits code, stores a baseline, or imposes a
workflow on the agent.

Install the MCP extra once, then register the STDIO server with the agent you
use:

```sh
python3 -m pip install '.[mcp]'
```

### Codex setup

```sh
codex mcp add codehealth -- codehealth-mcp
codex mcp list
codex mcp get codehealth
```

The final two commands confirm that Codex has the server registered and show
the command it will start. Start a new Codex session after registration so it
can discover the tools.

### Claude Code setup

```sh
claude mcp add --transport stdio codehealth -- codehealth-mcp
claude mcp list
claude mcp get codehealth
```

`claude mcp list` reports whether the server connected successfully. Inside a
Claude Code session, `/mcp` shows the server status and its discovered tool
count. See the
[official Claude Code MCP documentation](https://code.claude.com/docs/en/mcp)
for configuration scopes and troubleshooting.

### Tool discovery

Once connected, the client discovers `rank_java_files` and `score_java_files`,
including their argument schemas, descriptions, and read-only annotations.
The server instructions tell the agent when each tool is appropriate; there is
no separate tool catalog to maintain in the project. Claude Code may defer the
full tool definitions until a request needs them, which is normal.

If the server is registered but its tools are unavailable, verify that both
commands needed by the server are visible on `PATH`:

```sh
command -v codehealth-mcp
command -v pmd
```

### Usage from an agent

MCP tools are normally selected by the agent from a natural-language request.
Naming the tool is optional, but useful when the desired scope must be exact.
For cleanup discovery, ask for a recursive ranking:

> Use `rank_java_files` on `module-a/src/main/java` and
> `module-b/src/main/java`. Show the ten highest-scoring files and suggest the
> best cleanup target.

For feature work, have the agent derive the Java files in its change set and
pass that exact list to the scorer:

> Find the Java files changed by this branch and pass them to
> `score_java_files`. Report every score, then simplify the highest-scoring
> changed file if you can do so without changing behaviour. Score it again
> afterward.

For a known target, the request can be even simpler:

> Use `score_java_files` to score
> `src/main/java/example/OrderProcessor.java` before and after this refactor.

The feature agent does not have to finish below 100. A follow-up cleanup agent
can use the same exact-file score as its forcing function: make the score lower,
preserve behaviour, and keep working toward the target of 100.

Paths supplied to the tools are resolved from the MCP server's working
directory. See the
[Codex MCP documentation](https://learn.chatgpt.com/docs/extend/mcp) for other
configuration methods and MCP clients.

## Installation

Requirements:

- Python 3.10 or later
- Bash
- PMD 7, with the `pmd` executable on `PATH`

On macOS, PMD can be installed with Homebrew:

```sh
brew install pmd
```

On other platforms, install PMD using its
[official installation instructions](https://docs.pmd-code.org/latest/pmd_userdocs_installation.html).

Clone or download this repository, then either run `./codehealth` directly or
install it as a command:

```sh
python3 -m pip install .
```

For an isolated command installation, use `pipx install .`; add the `mcp` extra
when MCP support is wanted. A manual CLI-only installation is also possible:

```sh
mkdir -p "$HOME/.local/lib/codehealth" "$HOME/.local/bin"
install -m 755 codehealth codehealth.py "$HOME/.local/lib/codehealth/"
install -m 644 codehealth-ruleset.xml "$HOME/.local/lib/codehealth/"
ln -sf "$HOME/.local/lib/codehealth/codehealth" "$HOME/.local/bin/codehealth"
```

Make sure `$HOME/.local/bin` is on `PATH`, then verify the installation:

```sh
codehealth --help
```
