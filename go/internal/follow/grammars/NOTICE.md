# Embedded TextMate grammars

These grammar files are embedded in the `slopslap` binary and are used only
for source-view syntax highlighting. Each revision is pinned so builds do not
depend on the state of an upstream repository at build time.

| Language | File | Upstream | Revision |
| --- | --- | --- | --- |
| Go | `go.tmLanguage.json` | [Microsoft VS Code](https://github.com/microsoft/vscode/tree/main/extensions/go) | `main` at `28e27ffd3a0eca1de30b1a9656a75cbd47e94e4e` |
| Java | `java.tmLanguage.json` | [Red Hat vscode-java](https://github.com/redhat-developer/vscode-java) | `main` at `53806cd41757fe77262e1da6830b5ec6fb79e3a8` |
| TypeScript | `typescript.tmLanguage.json`, `typescriptreact.tmLanguage.json` | [Microsoft TypeScript-TmLanguage](https://github.com/microsoft/TypeScript-TmLanguage) via current VS Code conversions | grammar revision `48f608692aa6d6ad7bd65b478187906c798234a8` |
| Rust | `rust.tmLanguage.json` | [dustypomerleau/rust-syntax](https://github.com/dustypomerleau/rust-syntax) | `ca34cf382a7b250144f2ccb99ff9af96912c79f2` |

The upstream license and notice files are retained with the source checkout
used to update these assets. The tokenizer is `github.com/andersonpem/gopher-textmate`,
which is pure Go and uses no native runtime library.
