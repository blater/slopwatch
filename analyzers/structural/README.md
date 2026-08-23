# Slopslap structural analyzer

This Go module contains the language-neutral structural analyzer host, the
PMD-compatible metric strategies, and built-in Go, Java, and Rust source adapters.

The layers are deliberately separate:

```text
Go syntax adapter ─┐
Java JDK adapter ──┼─> normalized facts -> metric strategies -> protocol records
Rust syn adapter ──┘
```

The adapter parses every exact requested `.go` file once with `go/ast`, then
uses an offline `go/types` loader over those exact workspace sources and the Go
standard library. It translates functions, structured control flow, receiver
types, field accesses, and foreign type references into shared facts. The
strategies calculate cognitive complexity, cyclomatic complexity, NPath, deep
nesting, type WMC, CBO, and the WMC/ATFD/TCC inputs for responsibility concentration. NPath uses
arbitrary-size integers.

The loader never runs `go list`, builds repository packages, invokes generators,
or downloads modules. If an imported package is neither in the exact requested
source set nor the standard library, CBO and responsibility-concentration coverage is reported as
`unavailable` instead of emitting a misleading zero.

The Rust adapter uses `syn` in a bundled native helper and crosses a versioned
fact protocol. It parses only the requested `.rs` inventory and never invokes
Cargo, expands macros, runs build scripts, or loads dynamic plugins. Its facts
feed the same cognitive, cyclomatic, NPath, deep-if, WMC, CBO, and responsibility-concentration
strategies as Go. Rust support is classified as `supported` rather than
`conformant` because macro-generated control flow is intentionally outside the
source-level evidence boundary.

The Java adapter uses only the public JDK compiler-tree API with annotation
processing disabled. It parses the exact requested `.java` inventory without
building or executing project code. Its source-level facts feed the same seven
strategies; `--backend java=pmd` selects the retained PMD 7.26 bridge when the
classpath-aware oracle is preferred.

Build and test locally with:

```sh
go test ./...
javac --release 17 -d build/java-classes adapters/java/src/dev/slopslap/structural/*.java
jlink --add-modules java.base,jdk.compiler --strip-debug --no-man-pages --no-header-files --compress=2 --output java-runtime
cargo test --manifest-path adapters/rust/Cargo.toml
go build -o slopslap-structural ./cmd/slopslap-structural
```
