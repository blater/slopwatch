# Slopslap structural analyzer

This Go module contains the language-neutral structural analyzer host, the
PMD-compatible metric strategies, and built-in Go and Rust source adapters.

The layers are deliberately separate:

```text
Go syntax adapter ─┐
                   ├─> normalized facts -> metric strategies -> protocol records
Rust syn adapter ──┘
```

The adapter parses every exact requested `.go` file once with `go/ast`, then
uses an offline `go/types` loader over those exact workspace sources and the Go
standard library. It translates functions, structured control flow, receiver
types, field accesses, and foreign type references into shared facts. The
strategies calculate cognitive complexity, cyclomatic complexity, NPath, deep
nesting, type WMC, CBO, and the WMC/ATFD/TCC inputs for God Class. NPath uses
arbitrary-size integers.

The loader never runs `go list`, builds repository packages, invokes generators,
or downloads modules. If an imported package is neither in the exact requested
source set nor the standard library, CBO and God-Class coverage is reported as
`unavailable` instead of emitting a misleading zero.

The Rust adapter uses `syn` in a bundled native helper and crosses a versioned
fact protocol. It parses only the requested `.rs` inventory and never invokes
Cargo, expands macros, runs build scripts, or loads dynamic plugins. Its facts
feed the same cognitive, cyclomatic, NPath, deep-if, WMC, CBO, and God Class
strategies as Go. Rust support is classified as `supported` rather than
`conformant` because macro-generated control flow is intentionally outside the
source-level evidence boundary.

Build and test locally with:

```sh
go test ./...
cargo test --manifest-path adapters/rust/Cargo.toml
go build -o slopslap-structural ./cmd/slopslap-structural
```
