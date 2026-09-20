# Structural scoring calibration fixture

This directory is the compact, shared source corpus for structural SCORE
calibration (`structural-score-calibration-v1`).  Each case has a before/after
pair in Go, Java, TypeScript, and Rust.  The manifest is the machine-readable
contract; it freezes the relation to test before a scoring policy is changed.

The fixture is intentionally small and source-local.  It is suitable for
deterministic ordering and overlap tests, but it is not an enterprise
performance corpus.  Runtime claims (for example indexed lookup versus a
repeated scan) are recorded separately from maintainability relations.
