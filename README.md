# codehealth

`codehealth` ranks production Java files by design-debt signals reported by
[PMD](https://pmd.github.io/). It combines cognitive, cyclomatic, and NPath
complexity with nesting, coupling, and God Class findings to produce a
refactoring-priority score. The score is a triage aid, not a quality gate.

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

Show a compact top ten and include an explanation of the scoring model:

```sh
codehealth /path/to/java-project --compact --limit 10 --explain
```

For a non-standard project layout, specify one or more production source roots:

```sh
codehealth . --source-root app/java --source-root shared/java
```

Run `codehealth --help` for all options, including custom rulesets and an
explicit PMD Java language version.

## Installation

Requirements:

- Python 3.9 or later
- Bash
- PMD 7, with the `pmd` executable on `PATH`

On macOS, PMD can be installed with Homebrew:

```sh
brew install pmd
```

On other platforms, install PMD using its
[official installation instructions](https://docs.pmd-code.org/latest/pmd_userdocs_installation.html).

Clone or download this repository, then either run `./codehealth` directly or
install the three tool files under your home directory:

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
