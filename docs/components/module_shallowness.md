# Module shallowness

`module_shallowness/ousterhout-v2` is a file-scoped, 0–100 penalty measuring
useful functional capability per caller-visible interface cost. Its numerator
is COSMIC-inspired functional capability, not LOC or implementation
complexity; its denominator includes API surface, type cost, exposed state,
and Rising–Calliss information-hiding cost. The current implementation uses
the explicitly reported static approximation while adapters are extended with
full signature, data-group, persistence, and information-hiding facts. See
[`../depth-design.md`](../depth-design.md) and the
[`implementation plan`](../depth-implementation-plan.md).

The value is deliberately bounded and is an indicator for triage, not a claim
that a source file is itself a module in every architectural sense.
