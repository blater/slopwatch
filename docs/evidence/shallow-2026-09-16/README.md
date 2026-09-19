# Legacy SHALLOW probes, 2026-09-16

This archive preserves all 20 source probes and the installed analyzer's raw
output. These observations demonstrate current limitations; they are **not**
expected scores for the replacement metric or a depth-validation corpus.

- `sources.json`: complete source paths and contents, stored as data so a normal
  project scan does not accidentally analyze intentionally poor probe code.
- `baseline.json`: original complete report, including diagnostic/provenance data.
- `baseline-summary.json`: extracted raw SHALLOW observations and attributes.
- `manifest.json`: invocation, binary identity, baseline limitations and checksums.
- `replay.py`: materializes sources in a temporary directory and runs an installed
  analyzer, writing a new report without modifying this baseline.

From this directory:

```sh
python3 replay.py --binary slopmark --output /tmp/shallow-replayed.json
```

Use an unused output path. A new analyzer may produce different results; compare
normalized observations rather than invocation IDs. The script does not install
tools or execute fixture source code. Check the manifest to distinguish the
source checkout from the installed analyzer that produced the archived output.
