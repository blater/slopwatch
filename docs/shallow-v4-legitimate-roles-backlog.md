# SHALLOW: legitimate structural roles and actionable scores

Status: approved scope addition; shared supporting-contract rule implemented; adapter extraction and remaining rules pending.
Added: 2026-09-17, following the user's review of the River heap allocator.
Parent: [implementation specification](shallow-v4-implementation-spec.md).

The main display must distil the assessment into a useful number, N/A or unknown.
Users must not have to reconcile a high depth score with a separate architectural
role label. The detail popup supplies evidence for the decision already reflected
in the main display. Recognition uses resolved semantics and usage, never identifier
spelling, comments, suffixes or an assumed framework convention.

This is required before the new default is enabled. It amends the r6 policy for
applicability, boundary selection and recognized responsibility. The current r6
identity/constant/no-op golden scores are a baseline, not final acceptance of these
new cases. No runtime formula, threshold or default changes have been made by
adding this backlog. Publish the eventual policy/fixture changes under a new
policy identity and invalidate affected caches explicitly.

## Implementation backlog

- [ ] **Policy and applicability:** revise the policy table and golden cases for
  recognized legitimate roles. Distinguish independently assessable services,
  supporting contract implementations, passive data/creation, and unresolved
  evidence. Define each rule's evidence requirements and exact effect on the
  assessment. Apply rules to the relevant members/routes, not entire files or
  classes. Preserve high findings for understood extra burden with no recognized
  responsibility or structural justification. No arbitrary role discounts,
  confidence multipliers or blanket interface exemptions.
- [ ] **Compile-time protection:** recognize resolved nominal type distinctions,
  restricted construction and type-state constraints that enforce a supported
  caller-facing invariant. Distinguish these from interchangeable aliases and
  decorative wrappers. Specify the responsibility/identity and deduplication rule
  explicitly; do not count the same constraint again as runtime validation.
- [ ] **Contract implementations and substitution:** identify resolved required
  members, injected dependencies, optional callbacks and neutral implementations
  used through a contract. Assess the usable boundary and supplied behavior
  together; avoid independent penalties for mechanical supporting members.
  Existing tests may corroborate production bindings, but their presence, number
  or choice of mocks must not improve the production score. One visible
  implementation is not proof that substitution has no purpose.
- [ ] **Data, encapsulation and convenience:** extend the existing rules for
  passive records, allocation-only factories, inherited API, transparent packaging,
  restricted views, defensive copies and fixed-default overloads. Count the actual
  caller burden once. A getter exposing writable backing storage is a negative
  case, not evidence of encapsulation. A useful role does not erase unrelated
  leakage or other known burdens.
- [ ] **Delegation and adaptation:** follow resolved behavior, bindings, contract
  translations and representation conversions. Recognize the caller decisions
  actually removed by a convenience or integration boundary. Unsupported external
  behavior remains specifically unknown; do not invent benefit or assume H=0.
  Required protocol members alone are not proof of a meaningful independent service.
- [ ] **Evidence and presentation:** persist the applied rule, assessment effect,
  supporting symbol/binding/source locations and unresolved prerequisites. The
  main SHALLOW cell reflects the resulting assessment directly. The info popup
  explains why the rule applied and which boundary owns any associated score.
  N/A is distinct from measured zero and from unknown. These distinctions must
  survive aggregate SCORE, sorting, graph populations and fix verification.
- [ ] **Four-language conformance and cache integration:** add common positive and
  negative role fixtures plus native Go/Java/Rust/TypeScript variants where the
  semantics exist. Track dependencies of applicability evidence as well as numeric
  responsibility evidence. Relevant contract, export, binding or access changes
  invalidate consumers through cached indexes; no UI-time or idle whole-repo scans.

## Required regression pairs

| Positive case | Counterexample / protection |
| --- | --- |
| Constructor-injected internal allocator with resolved contract usage | Trivial independently exposed wrapper with no such binding or other recognized role |
| No-op satisfying an optional callback/default contract | Unrelated public no-op method; no class-wide exemption |
| Identity used as a resolved neutral operation in composition | Standalone identity wrapper; spelling alone changes nothing |
| Nominal type or restricted construction prevents an invalid use | Interchangeable alias or publicly bypassable claimed invariant |
| Resolved contract translation or useful delegation | Extra forwarding layers that retain the same burden; unknown callee is not assumed useful or useless |
| Read-only view or supported defensive copy | Returned writable backing object or shallow copy retaining nested mutable aliases |
| Passive carrier / allocation-only creation | Behavior-bearing service with extra obligations, failures or lifecycle work |
| Fixed defaults simplify access to the same service | An additional independent operation; no fabricated family merging |

Given each pair, analysis must produce the specified numeric/applicability outcome
and matching detail evidence, not merely attach a reassuring role label to the old
high score. For a mixed class, the protected member must not suppress a finding on
an unrelated member. Equivalent source must preserve assessments under identifier
renaming, helper relocation, lambda/class conversion and input order changes.
Adding/removing test doubles must not flip production eligibility or scores.
Missing context must preserve known evidence and report the particular essential
gap rather than fabricate a high/low result. Unknown architectural intent by itself
is not a reason to abstain on every otherwise supported service.

## Concrete River regression

Source checkout: `/Users/blater/src/river/river-engine`.

`HeapPendingRowChunkAllocator` and `PendingRowChunkAllocator` are package-private.
`PendingRowArena` receives the contract through a constructor; the normal provider
only creates a byte array. `PendingMutationBufferTest` supplies an alternate
implementation and exercises allocation failure followed by successful retry.

Expected: no independent high SHALLOW verdict on the internal allocation provider;
retain its behavior as supporting evidence for the actual usable boundary. Detail
must identify the internal allocation/substitution role and production binding.
The test provides corroborating detail, not the basis for a numeric discount.
Include a public factory/interface exposure variant to verify that nonpublic
implementation types are not simply erased from an externally usable contract.

## Delivery order

Finalize the rule/expected-outcome table before responsibility recognition and
boundary assembly are accepted. Add normalized role facts to the common contract;
implement the shared rules before adapter-specific extraction. Deliver native
fixtures with each adapter, and the explanation/aggregation behavior with the
existing ledger/profile/UI batches. Include the River case in final real-repository
validation. This is ordinary implementation and regression work, with no separate
research phase or external reviewer prerequisite.

## Implemented shared rule: supporting-contract-v1

The opt-in scoring policy is now `r7`; the integer formula, responsibility weights,
and all non-SHALLOW metric definitions remain unchanged. This revision adds the
following applicability rule; it does not claim completion of the remaining roles.

| Required evidence | Assessment effect |
| --- | --- |
| Complete candidate-member inventory and external-exposure inventory; no direct or indirect external routes; every candidate member bound to an exact contract member with a resolved production consumer, injection slot and use site | The candidate boundary is N/A. Preserve its flow for consumers, retain contract/use dependencies, and include the binding in detail evidence. No numerical responsibility bonus. |
| Public factory/contract exposure, any independent route, unmatched member, incomplete exposure, or missing production use | Do not apply this exemption. Continue the ordinary assessment, including partial where required semantics remain unsupported. |

Normalized evidence is `BoundaryAssessment.supporting_contract`; it contains
relationships and inventory completeness, not names, a confidence score, or an
adapter-awarded responsibility. Test-only bindings cannot populate production uses.
The rule is shared by flow evaluation and standalone boundary scoring. Adapter
extraction is still required before the River allocator regression is resolved.
