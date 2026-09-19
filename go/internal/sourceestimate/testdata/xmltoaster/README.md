# Actual xmltoaster lookup/formatting regression

QueryResultRow.java is an exact source snapshot captured on 2026-09-18 from
`/Users/blater/src/xmltoaster/xmltoaster/src/main/java/blater/nestql/runner/sql/domain/QueryResultRow.java`.

SHA-256: `7b8327b09f7f8b0b512aab2cdf2d05b18b84c6bf43b33c46079af9c3b4520823`.

This version uses conditional-expression formatting and `newValue()`. It is
separate from the frozen NQL benchmark, which uses switch-based formatting,
decimal handling and `getScalarKind()`. Both must be tested; neither substitutes
for the other. The source is retained unchanged, including its original package
and imports. Missing dependencies are intentionally observable in fallback tests.
