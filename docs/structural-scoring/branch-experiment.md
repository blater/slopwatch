# Plan 4 branch evidence experiment

Status: **no ship for the Plan 4 interaction candidate**. The probe changed no
scoring code, parameters, or holdout membership. It used the existing
adapter-owned facts directly for Go, Java, and Rust, and the compiled
TypeScript analyzer's existing `FunctionFact` walk. The bounded results,
source hashes, probe hashes, and the earlier protocol payload measurements are
in the [machine evidence](../evidence/structural-scoring/branch-experiment-v1.json).

The exact two dispatch files were walked with a limit of 128 instructions or
AST nodes and 64 control regions per function. Every walk completed within the
limits. The direct adapter results were:

| Language | Functions | Flow artifacts / functions / blocks / instructions | Canonical field reads / writes | Unknown ops | Evidence |
| --- | ---: | ---: | ---: | ---: | --- |
| Go | 2 | 1 / 2 / 14 / 22 | 0 / 0 | 1 | walk complete; flow partial |
| Java | 2 | 1 / 4 / 16 / 18 | 0 / 0 | 6 | walk complete; flow partial |
| Rust | 2 | 2 / 2 / 2 / 4 | 0 / 0 | 2 | walk complete; flow partial |
| TypeScript | 2 | FunctionFact only | unavailable | n/a | complete AST walk |

The normalized statement walks do distinguish the flat switch from the
interacting `if` chain: the flat functions have one region and the interacting
functions have four. That is existing control-flow evidence covered by Plan 3,
so it supplies no new interaction signal. Go, Java, and Rust measured zero
`field_read`/`field_write` operations. The examples mutate local variables;
zero field operations therefore mean “no field evidence emitted,” not “proof
that the locals are independent.” TypeScript `FunctionFact` contains only
`node`, `name`, and `subject`, so its canonical read/write result is missing;
syntax property counts were not substituted for canonical state evidence.

The bounded experiment cannot safely distinguish shared local-state coupling
from independent dispatch using the available Plan 4 representation. Missing
evidence retains the Plan 3 burden and earns no interaction discount or bonus.
The smallest useful follow-up is an adapter-owned local-state or canonical
flow signal with explicit ordering and conservative unknown handling; that is
outside this calibration artifact and requires a separately reviewed change.

The temporary Go test and TypeScript probe source remain under
`/tmp/structural-branch-experiment/` for reproduction. The repository test was
removed after the required `make build` run passed. No additional test or build
command was run for this artifact.
