# Situational depth-reference test plan

## Scope

Validate role classification, conservative fallback, reference selection, and
cross-language consistency. These tests are intentionally small and targeted;
they do not attempt to prove that the eventual calibration corpus is
representative.

## Fixtures

Create one minimal fixture per role in each applicable analyzer:

- protocol: public Java interface and Rust trait with lower-case methods;
- command: public operations with inputs and status/void outputs;
- query: public operations returning data;
- data: public type/state surface with no operations;
- general: mixed operations that satisfy no stronger rule;
- unknown: incomplete or contradictory surface evidence.

Include equivalent Go interfaces/functions and TypeScript exported interfaces,
classes, and functions where the language supports the role.

## Assertions

1. Java interface methods without an explicit `public` modifier are public.
2. Rust methods in a public trait are public operations; methods in a private
   trait are not.
3. Go exported identifiers are public; lower-case identifiers are not.
4. TypeScript class/interface members without `private` or `protected` are
   public, while unexported top-level declarations are not module surface.
5. A surface with no public-operation evidence is unavailable, never a score
   inferred from capitalization.
6. Equivalent fixtures produce the same role where their normalized facts are
   equivalent.
7. Unknown or ambiguous evidence stays at `D_ref = 1.0`.
8. Role and confidence changes select the documented role-shaped reference and
   report its policy and sample count.
9. Small or low-confidence surfaces shrink toward `D_ref = 1.0`.
10. Adding unnecessary private implementation does not change role, `F`, `I`,
    or `D_ref`.

## Acceptance criteria

- all fixtures are deterministic and language-specific visibility rules are
  tested directly;
- no role-specific numeric reference is introduced without corpus evidence;
- missing evidence is explicit and cannot become a false shallow score;
- structural and TypeScript analyzers expose the same attribute names;
- the existing River interface examples remain explainable from their
  reported `F`, `I`, and global reference.
