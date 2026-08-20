# Slopscout PMD bridge

This analyzer-side rule emits raw PMD 7.26.0 metric values for every Java
method, constructor, record constructor, class, interface, record, and enum. Values are
reported even when they are below the active Slopscout scoring threshold.

Interface methods receive the same executable complexity measurements as class
methods. Interface type complexity uses PMD cyclomatic values aggregated across
its operations, and interface fan-out uses PMD directly. PMD's class-cohesion
metric is not applied to interfaces, so the class-specific `god_class`
measurement is omitted for them.

Record compact constructors are analyzed with the same pinned PMD cognitive and
cyclomatic visitors used for ordinary executable bodies, plus PMD's public
NPath metric.

Build the bridge:

```sh
mvn -q -f analyzers/java/pom.xml package
```

For development with an installed PMD distribution, add the bridge JAR to the
Java classpath and run PMD with `slopscout-bridge-ruleset.xml`. Analyzed
projects do not receive this dependency.

Each `SlopscoutMetrics` violation message begins with
`SLOPSCOUT_METRIC_V1` and contains a stable pipe-delimited observation. The
process adapter is responsible for strict decoding and conversion to the common
NDJSON measurement protocol. PMD's built-in `AvoidDeeplyNestedIfStmts` remains
in the bridge ruleset so nesting findings retain oracle behavior.
