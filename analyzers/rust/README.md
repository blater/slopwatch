# Safe Rust analyzer

`slopscout-rust-structural` reads exact `.rs` source paths from protocol v1 and
emits only `rust_design` measurements: `rust_large_function`,
`rust_deep_nesting`, `rust_unsafe_block`, and `rust_panic_macro`, all version
`rust-v1`. It intentionally emits no PMD-aligned component identity: conformance
has not yet been established for Rust semantics.

The parser is lexical and does not expand macros. Macro-generated control flow
is therefore not measured. `cargo metadata` is available only when the request
sets `options.cargo_metadata=true` and supplies the analyzer-owned
`options.cargo_path`; it runs exactly `metadata --offline --no-deps
--format-version 1` with null standard input and never invokes a build, scripts,
proc macros, Clippy, or a project-selected Cargo executable.
