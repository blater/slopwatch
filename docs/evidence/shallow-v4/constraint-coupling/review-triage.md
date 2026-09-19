# Constraint correction review triage

Three independent review layers inspected the exact task diff against the pre-task working files: blind review, edge-case review and verification-gap review. Existing unrelated changes were excluded. The specification already requires connected constraints, storage identity, observable work, normalization and useful numeric publication; no intent change or weight adjustment is required.

| Finding | Consequence | Route | Required correction |
|---|---|---|---|
| Indexed writes skipped | High | patch | Recognize bounds of both reads and writes, retaining map exclusion. |
| Protected constant/parameter index charged | High | patch | Prove bounded protection or retain uncertainty rather than assert unsupported burden. |
| Size equality admission omitted | Medium | patch | Retain actual size-consistency constraints without duplicating discharged bounds. |
| Deferred/dead bodies supply constraints or phases | High | patch | Restrict witnesses to eagerly executed connected behavior. |
| Data-only storage absent from registry | High | patch | Resolve declarations independently of irrelevant methods. |
| Relational consumers omitted outside Java / equality omitted | Medium | patch | Follow exact typed parameter bindings and relational rejection consistently. |
| Boolean expression prefix mistaken for exact phase | High | patch | Require exact resolved Boolean assignment. |
| Local declarations treated as package shadows | Medium | patch | Respect declaration scope. |
| Size-derived count credited for wrong index expression | High | patch | Check the actual indexed expression and required relationship. |
| Maintenance/protection survives invalidation | High | patch | Invalidate on later writes, receiver replacement and relevant unresolved mutation. |
| Go receiver mutability resolved by method name alone | High | patch | Use owner/declaration identity. |
| Unrelated returned call gives discarded switch credit | High | patch | Require branch-specific data flow into observable output. |
| Aggregate estimate tests assert only absence of burden | High | patch | Assert transformation-only allowance, deduplication and disconnected negatives. |

Duplicate findings from the review layers were merged only when the claim and required correction matched. Implementation corrections and regression evidence are recorded in the final review report. The known pre-existing `analysiscache.TestProjectionArtifactRoundTrip` nil-versus-empty depth-map test failure is outside this correction and is retained in the validation record.

## Resolution

All patch findings above are implemented and covered by `graded_constraint_review_test.go`, the expanded core constraint tests and native evidence tests. Targeted follow-up also found and corrected Go parallel-assignment invalidation and parameterized Rust move-closure filtering. Follow-up reviewers confirmed both fixes and the aggregate/variant verification gaps closed within their reviewed scope. Final focused suites, race checks and both packaged acceptance suites pass; retained real holdout failures and the pre-existing full-Go failure remain explicit.
